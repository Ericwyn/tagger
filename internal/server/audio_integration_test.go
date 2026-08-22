package server

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags/taglibwasm"
)

func TestAudioAPIWithCopiedTestMusic(t *testing.T) {
	corpus := os.Getenv("TAGGER_TEST_MUSIC_DIR")
	if corpus == "" {
		corpus = "/home/ericwyn/Downloads/TestMusic"
	}
	for _, fixture := range []struct {
		name        string
		extension   string
		contentType string
	}{
		{name: "mp3", extension: ".mp3", contentType: "audio/mpeg"},
		{name: "flac", extension: ".flac", contentType: "audio/flac"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := findIntegrationAudio(t, corpus, fixture.extension)
			root := t.TempDir()
			destination := filepath.Join(root, filepath.Base(source))
			copyIntegrationFile(t, source, destination)

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
			s := New("127.0.0.1:0", service, writer, providers.NewRegistry(serverProvider{}), dataStore,
				fs.FS(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("Tagger")}}), "test", engine.Version())
			track := service.ListTracks(library.TrackFilter{})[0]
			ref, err := service.FileRef(track.ID)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := os.ReadFile(ref.AbsolutePath)
			if err != nil {
				t.Fatal(err)
			}
			end := 31
			if len(payload) <= end {
				end = len(payload) - 1
			}
			if end < 0 {
				t.Fatal("fixture is empty")
			}
			rangeEnd := strconv.Itoa(end)
			response := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/audio", nil,
				ut.Header{Key: "Range", Value: "bytes=0-" + rangeEnd})
			if response.Code != 206 || response.Result().Header.Get("Content-Type") != fixture.contentType ||
				response.Result().Header.Get("Content-Range") != "bytes 0-"+rangeEnd+"/"+strconv.Itoa(len(payload)) ||
				!bytes.Equal(response.Body.Bytes(), payload[:end+1]) {
				t.Fatalf("audio response = %d contentRange=%q contentType=%q body=%d bytes", response.Code,
					response.Result().Header.Get("Content-Range"), response.Result().Header.Get("Content-Type"), response.Body.Len())
			}
		})
	}
}
