package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeRootReportsSupportedAudioAndPermissions(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Album"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "one.mp3"),
		filepath.Join(root, "Album", "two.FLAC"),
		filepath.Join(root, "Album", "three.wav"),
		filepath.Join(root, "cover.jpg"),
	} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	probe, err := ProbeRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if !probe.Readable || !probe.Writable || probe.AudioFiles != 3 || probe.Folders != 1 {
		t.Fatalf("probe = %#v", probe)
	}
	if probe.Formats["mp3"] != 1 || probe.Formats["flac"] != 1 || probe.Formats["wav"] != 1 {
		t.Fatalf("formats = %#v", probe.Formats)
	}
}

func TestProbeRootRejectsSymlinkAndFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "song.mp3")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeRoot(file); err == nil {
		t.Fatal("expected a regular file to be rejected")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ProbeRoot(link); err == nil {
		t.Fatal("expected a symlink root to be rejected")
	}
}

func TestProbeTestMusicCorpus(t *testing.T) {
	root := os.Getenv("TAGGER_TEST_MUSIC_DIR")
	if root == "" {
		t.Skip("TAGGER_TEST_MUSIC_DIR is not set")
	}
	probe, err := ProbeRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if probe.AudioFiles == 0 || probe.Formats["mp3"]+probe.Formats["flac"]+probe.Formats["wav"] != probe.AudioFiles {
		t.Fatalf("unexpected TestMusic probe = %#v", probe)
	}
}
