package jobs_test

import (
	"context"
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
