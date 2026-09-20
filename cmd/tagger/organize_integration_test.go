package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/jobs"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags"
)

type organizeTestEngine struct{}

func (organizeTestEngine) Read(context.Context, string) (tags.Snapshot, error) {
	return tags.Snapshot{Raw: map[string][]string{
		"TITLE": {"Song"}, "ARTIST": {"歌手"}, "ALBUM": {"专辑"}, "ALBUMARTIST": {"歌手"},
	}}, nil
}

func (organizeTestEngine) Write(context.Context, string, map[string][]string) error { return nil }
func (organizeTestEngine) Version() string                                          { return "organize-test" }

func TestOrganizeHandlerMovesFilesAndPreservesTrackID(t *testing.T) {
	root := t.TempDir()
	oldAudio := filepath.Join(root, "incoming", "song.mp3")
	if err := os.MkdirAll(filepath.Dir(oldAudio), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldAudio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "incoming", "song.lrc"), []byte("lyrics"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := organizeTestEngine{}
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service, err := library.New(context.Background(), musicScanner, repository)
	if err != nil {
		t.Fatal(err)
	}
	track := service.ListTracks(library.TrackFilter{})[0]
	manager := jobs.New(repository)
	manager.Register(domain.JobOrganize, newOrganizeHandler(service, repository))
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	payload := domain.OrganizePayload{MoveLyricsSidecar: true, Items: []domain.OrganizeItemRequest{{TrackID: track.ID, BaseRevision: track.Revision}}}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobOrganize, Title: "整理", Total: 1, Payload: string(payloadJSON)})
	if err != nil {
		t.Fatal(err)
	}
	job := waitBatchEditJob(t, manager, created.ID)
	if job.State != domain.JobSucceeded || job.Succeeded != 1 || job.Failed != 0 {
		t.Fatalf("organize job = %#v", job)
	}
	newAudio := filepath.Join(root, "歌手", "专辑", "song.mp3")
	if _, err := os.Stat(newAudio); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "歌手", "专辑", "song.lrc")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldAudio); !os.IsNotExist(err) {
		t.Fatalf("old audio still exists: %v", err)
	}
	updated, err := service.Track(track.ID)
	if err != nil || updated.ID != track.ID || updated.RelativePath != "歌手/专辑/song.mp3" {
		t.Fatalf("relocated index = %#v err=%v", updated, err)
	}
	items, err := repository.ListOrganizeItems(context.Background(), created.ID)
	if err != nil || len(items) != 1 || items[0].State != domain.OrganizeMoved {
		t.Fatalf("organize items = %#v err=%v", items, err)
	}
}
