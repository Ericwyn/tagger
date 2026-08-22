package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/jobs"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags/taglibwasm"
)

func TestBatchEditWorkerWithCopiedTestMusic(t *testing.T) {
	corpus := os.Getenv("TAGGER_TEST_MUSIC_DIR")
	if corpus == "" {
		corpus = "/home/ericwyn/Downloads/TestMusic"
	}
	source := findBatchEditFixture(t, corpus)
	root := t.TempDir()
	destination := filepath.Join(root, filepath.Base(source))
	copyBatchEditFixture(t, source, destination)

	engine := taglibwasm.New()
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service, err := library.New(context.Background(), musicScanner, dataStore)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := filewrite.New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	track := service.ListTracks(library.TrackFilter{})[0]
	manager := jobs.New(dataStore)
	manager.Register(domain.JobBatchEdit, newBatchEditHandler(service, writer, dataStore))
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	replacedTitle := track.Title + " · 清理"
	payload := domain.BatchEditPayload{
		Items: []domain.BatchEditItem{{TrackID: track.ID, BaseRevision: track.Revision}},
		Operations: []domain.BatchEditOperation{
			{Field: "genres", Mode: domain.BatchEditAppend, Value: "Live"},
			{Field: "title", Mode: domain.BatchEditReplace, Find: track.Title, Value: replacedTitle},
		},
		SequenceTracks: true,
		Artwork: &domain.BatchArtwork{
			Action:  domain.BatchArtworkReplace,
			Data:    batchArtworkData(t),
			MIME:    "image/png",
			MaxSize: 500,
		},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobBatchEdit, LibraryID: service.Library().ID, Title: "Batch edit", Payload: string(payloadJSON), Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	job := waitBatchEditJob(t, manager, created.ID)
	if job.State != domain.JobSucceeded || job.Succeeded != 1 {
		t.Fatalf("job = %#v", job)
	}
	items, err := dataStore.ListBatchEditItems(context.Background(), created.ID)
	if err != nil || len(items) != 1 || items[0].State != "written" || !strings.Contains(string(items[0].Diff), "genres") || !strings.Contains(string(items[0].Diff), "artwork") {
		t.Fatalf("items = %#v err=%v", items, err)
	}
	updated, err := service.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != replacedTitle || updated.TrackNumber == nil || *updated.TrackNumber != 1 || updated.TrackTotal == nil || *updated.TrackTotal != 1 || !slices.Contains(updated.Genres, "Live") || updated.ArtworkWidth != 500 || updated.ArtworkHeight != 500 {
		t.Fatalf("updated track = %#v", updated)
	}
	revisions, err := dataStore.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 1 || revisions[0].Action != "批量编辑标签与封面" {
		t.Fatalf("revisions = %#v err=%v", revisions, err)
	}
}

func batchArtworkData(t *testing.T) string {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 1200, 1000))
	for y := 0; y < canvas.Bounds().Dy(); y++ {
		for x := 0; x < canvas.Bounds().Dx(); x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 90, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(encoded.Bytes())
}

func waitBatchEditJob(t *testing.T, manager *jobs.Manager, id string) domain.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == domain.JobSucceeded || job.State == domain.JobPartial || job.State == domain.JobFailed || job.State == domain.JobCancelled {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", id)
	return domain.Job{}
}

func findBatchEditFixture(t *testing.T, root string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".mp3") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil || found == "" {
		t.Skipf("TestMusic MP3 fixture unavailable under %s: %v", root, err)
	}
	return found
}

func copyBatchEditFixture(t *testing.T, source, destination string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
