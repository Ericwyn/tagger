package organizer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ericwyn/tagger/internal/domain"
)

func TestPlanBuildsArtistAlbumPathAndMovesSidecarReference(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Downloads", "song.flac"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Downloads", "song.lrc"), []byte("lyrics"), 0o644); err != nil {
		t.Fatal(err)
	}
	planner, err := NewPlanner(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Plan(context.Background(), domain.Track{
		ID: "trk-1", FileName: "song.flac", RelativePath: "Downloads/song.flac",
		Artists: []string{"许嵩 / 其他歌手"}, Album: "苏格拉没有底",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target != "许嵩/苏格拉没有底/song.flac" || plan.PrimaryArtist != "许嵩" || !plan.SidecarExists {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.SidecarTarget != "许嵩/苏格拉没有底/song.lrc" {
		t.Fatalf("sidecar target = %q", plan.SidecarTarget)
	}
}

func TestPlanBuildsArtistOnlyPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.flac"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	planner, err := NewPlanner(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.PlanWithMode(context.Background(), domain.Track{
		ID: "trk-artist", FileName: "song.flac", RelativePath: "song.flac",
		Artists: []string{"许嵩 / 其他歌手"}, Album: "苏格拉没有底",
	}, domain.OrganizeModeArtist, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target != "许嵩/song.flac" || plan.PrimaryArtist != "许嵩" || plan.Album != "苏格拉没有底" {
		t.Fatalf("artist-only plan = %#v", plan)
	}
}

func TestPlanUsesCurrentDirectoryAsOrganizationBase(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "M", "incoming"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "M", "incoming", "song.flac"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	planner, err := NewPlanner(root)
	if err != nil {
		t.Fatal(err)
	}
	track := domain.Track{ID: "trk-base", FileName: "song.flac", RelativePath: "M/incoming/song.flac", Artists: []string{"歌手"}, Album: "专辑"}
	plan, err := planner.PlanWithBasePath(context.Background(), track, "M", domain.OrganizeModeArtistAlbum, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target != "M/歌手/专辑/song.flac" {
		t.Fatalf("base path target = %q", plan.Target)
	}
	if _, err := planner.PlanWithBasePath(context.Background(), domain.Track{ID: "outside", FileName: "song.flac", RelativePath: "Other/song.flac", Artists: []string{"歌手"}, Album: "专辑"}, "M", domain.OrganizeModeArtistAlbum, false); err == nil {
		t.Fatal("track outside the selected base path was accepted")
	}
}

func TestPlanPreservesArtistNamesThatUseSlashAndAmpersand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	planner, err := NewPlanner(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Plan(context.Background(), domain.Track{
		ID: "trk-2", FileName: "song.mp3", RelativePath: "song.mp3",
		Artists: []string{"AC/DC & Friends"}, Album: "Live",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PrimaryArtist != "AC_DC & Friends" || plan.Target != "AC_DC & Friends/Live/song.mp3" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanDetectsExistingAndDuplicateTargets(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "歌手", "专辑"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.flac"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.flac"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "歌手", "专辑", "a.flac"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	planner, err := NewPlanner(root)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := planner.PlanMany(context.Background(), []domain.Track{
		{ID: "a", FileName: "a.flac", RelativePath: "a.flac", Artists: []string{"歌手"}, Album: "专辑"},
		{ID: "b", FileName: "a.flac", RelativePath: "b.flac", Artists: []string{"歌手"}, Album: "专辑"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].State != domain.OrganizeConflict || plans[1].State != domain.OrganizeConflict {
		t.Fatalf("plans = %#v", plans)
	}
}

func TestPlanRejectsOutsideAndSymlinkPaths(t *testing.T) {
	root := t.TempDir()
	planner, err := NewPlanner(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planner.Plan(context.Background(), domain.Track{ID: "bad", FileName: "song.mp3", RelativePath: "../song.mp3", Artists: []string{"歌手"}, Album: "专辑"}, false); !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("outside path error = %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.mp3")
	if err := os.WriteFile(outside, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.mp3")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	plan, err := planner.Plan(context.Background(), domain.Track{ID: "link", FileName: "link.mp3", RelativePath: "link.mp3", Artists: []string{"歌手"}, Album: "专辑"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.State != domain.OrganizeInvalid {
		t.Fatalf("symlink plan = %#v", plan)
	}
}

func TestMoveAndRollbackKeepAudioAndSidecarTogether(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old", "song.flac"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old", "song.lrc"), []byte("lyrics"), 0o644); err != nil {
		t.Fatal(err)
	}
	planner, err := NewPlanner(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Plan(context.Background(), domain.Track{ID: "move", FileName: "song.flac", RelativePath: "old/song.flac", Artists: []string{"歌手"}, Album: "专辑"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := Move(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "歌手", "专辑", "song.flac")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "歌手", "专辑", "song.lrc")); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "old", "song.flac")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "old", "song.lrc")); err != nil {
		t.Fatal(err)
	}
}
