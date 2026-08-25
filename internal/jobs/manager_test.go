package jobs_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/jobs"
	"github.com/ericwyn/tagger/internal/store"
)

func TestManagerExecutesAndPersistsJob(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	manager.Register(domain.JobScan, func(_ context.Context, _ domain.Job, progress jobs.Progress) error {
		return progress(3, 3, 3, 0, "扫描完成")
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	created, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobScan, Title: "Test scan", Detail: "waiting"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.Get(context.Background(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == domain.JobSucceeded {
			if job.Processed != 3 || job.Succeeded != 3 || job.Detail != "扫描完成" {
				t.Fatalf("job = %#v", job)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not complete")
}

func TestManagerCancelsRunningJobAndPublishesSnapshots(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	started := make(chan struct{})
	manager.Register(domain.JobScan, func(ctx context.Context, _ domain.Job, _ jobs.Progress) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	events, unsubscribe := manager.Subscribe("")
	defer unsubscribe()
	created, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobScan, Title: "Cancelable", Detail: "waiting", Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}

	if _, err := manager.Cancel(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	seenCancelled := false
	for time.Now().Before(deadline) {
		select {
		case event := <-events:
			if event.Job.ID == created.ID && event.Job.State == domain.JobCancelled {
				seenCancelled = true
			}
		case <-time.After(20 * time.Millisecond):
		}
		if seenCancelled {
			break
		}
	}
	if !seenCancelled {
		t.Fatal("cancelled event was not published")
	}
	job, err := manager.Get(context.Background(), created.ID)
	if err != nil || job.State != domain.JobCancelled {
		t.Fatalf("job = %#v err=%v", job, err)
	}
}

func TestManagerRetriesFailedJob(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	attempts := 0
	manager.Register(domain.JobScan, func(_ context.Context, _ domain.Job, progress jobs.Progress) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary failure")
		}
		return progress(1, 1, 1, 0, "重试完成")
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	created, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobScan, Title: "Retryable", Detail: "waiting", Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager, created.ID, domain.JobFailed)
	retried, err := manager.Retry(context.Background(), created.ID, "")
	if err != nil || retried.State != domain.JobWaiting {
		t.Fatalf("retry = %#v err=%v", retried, err)
	}
	job := waitForState(t, manager, created.ID, domain.JobSucceeded)
	if job.Succeeded != 1 || attempts != 2 {
		t.Fatalf("retried job = %#v attempts=%d", job, attempts)
	}
}

func TestReviewJobsDoNotBlockFilesystemRefresh(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	review, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobMatch, Title: "Review", Detail: "review"})
	if err != nil {
		t.Fatal(err)
	}
	review.State = domain.JobReview
	if err := repository.UpdateJob(context.Background(), review); err != nil {
		t.Fatal(err)
	}
	active, err := manager.HasActive(context.Background())
	if err != nil || !active {
		t.Fatalf("review active=%v err=%v", active, err)
	}
	blocking, err := manager.HasBlockingFileWork(context.Background())
	if err != nil || blocking {
		t.Fatalf("review blocking=%v err=%v", blocking, err)
	}
	if _, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobScan, Title: "Scan", Detail: "waiting"}); err != nil {
		t.Fatal(err)
	}
	blocking, err = manager.HasBlockingFileWork(context.Background())
	if err != nil || !blocking {
		t.Fatalf("scan blocking=%v err=%v", blocking, err)
	}
}

func TestManagerSerializesRecoveryScanBehindRunningWrite(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	scanStarted := make(chan struct{})
	order := make(chan string, 2)
	manager.Register(domain.JobWrite, func(_ context.Context, _ domain.Job, _ jobs.Progress) error {
		close(writeStarted)
		<-releaseWrite
		order <- "write"
		return nil
	})
	manager.Register(domain.JobScan, func(_ context.Context, _ domain.Job, _ jobs.Progress) error {
		order <- "scan"
		close(scanStarted)
		return nil
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			close(releaseWrite)
		}
		manager.Close()
	}()
	if _, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobWrite, Title: "Persisted write"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("write job did not start")
	}
	if _, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobScan, Title: "Draft recovery scan"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-scanStarted:
		t.Fatal("recovery scan started before the running write completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseWrite)
	released = true
	for _, want := range []string{"write", "scan"} {
		select {
		case got := <-order:
			if got != want {
				t.Fatalf("execution order got %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s job", want)
		}
	}
}

func waitForState(t *testing.T, manager *jobs.Manager, id string, state domain.JobState) domain.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == state {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", id, state)
	return domain.Job{}
}
