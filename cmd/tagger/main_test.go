package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/providers"
)

type artworkTestStrategy struct{}

func (artworkTestStrategy) Descriptor() providers.Descriptor {
	return providers.Descriptor{ID: "artwork-test", Name: "Artwork Test", Enabled: true, Health: providers.HealthReady}
}

func (artworkTestStrategy) Search(context.Context, providers.Query, int) ([]providers.Candidate, error) {
	return []providers.Candidate{{ProviderID: "artwork-test", ExternalID: "external-1", Title: "Song", ArtworkURL: "https://images.example.test/cover.jpg"}}, nil
}

func TestPatchFromCandidateDistinguishesOmittedAndEmptyFieldLists(t *testing.T) {
	candidate := providers.MatchCandidate{
		Title:     providers.Field[string]{Value: "新标题"},
		DiscTotal: providers.Field[int]{Value: 2},
		Lyrics:    &providers.Field[string]{Value: "歌词"},
	}

	allFields := patchFromCandidate(candidate, nil)
	if allFields.Title == nil || allFields.DiscTotal == nil || allFields.Lyrics == nil {
		t.Fatalf("nil fields should preserve the backwards-compatible all-fields behavior: %#v", allFields)
	}

	noFields := patchFromCandidate(candidate, []string{})
	if noFields.Title != nil || noFields.DiscTotal != nil || noFields.Lyrics != nil {
		t.Fatalf("an explicit empty field list must produce a no-op patch: %#v", noFields)
	}

	selected := patchFromCandidate(candidate, []string{"title"})
	if selected.Title == nil || selected.DiscTotal != nil || selected.Lyrics != nil {
		t.Fatalf("selected fields must be applied precisely: %#v", selected)
	}
}

func TestPatchFromCandidateIncludesSelectedExtendedFields(t *testing.T) {
	candidate := providers.MatchCandidate{
		Comment:              providers.Field[string]{Value: "liner note"},
		Composers:            providers.Field[[]string]{Value: []string{"Composer"}},
		Conductor:            providers.Field[string]{Value: "Conductor"},
		Lyricists:            providers.Field[[]string]{Value: []string{"Lyricist"}},
		Copyright:            providers.Field[string]{Value: "© Tagger"},
		BPM:                  providers.Field[int]{Value: 128},
		ISRC:                 providers.Field[string]{Value: "US-TAG-26-00001"},
		MusicBrainzTrackID:   providers.Field[string]{Value: "track-mbid"},
		MusicBrainzReleaseID: providers.Field[string]{Value: "release-mbid"},
		MusicBrainzArtistIDs: providers.Field[[]string]{Value: []string{"artist-mbid"}},
		AcoustID:             providers.Field[string]{Value: "acoustid-id"},
		AcoustIDFingerprint:  providers.Field[string]{Value: "fingerprint"},
	}
	patch := patchFromCandidate(candidate, []string{"comment", "composers", "conductor", "lyricists", "copyright", "bpm", "isrc", "musicbrainzTrackId", "musicbrainzReleaseId", "musicbrainzArtistIds", "acoustidId", "acoustidFingerprint"})
	if patch.Comment == nil || patch.Composers == nil || patch.Conductor == nil || patch.Lyricists == nil || patch.Copyright == nil || patch.BPM == nil || patch.ISRC == nil || patch.MusicBrainzTrackID == nil || patch.MusicBrainzReleaseID == nil || patch.MusicBrainzArtistIDs == nil || patch.AcoustID == nil || patch.AcoustIDFingerprint == nil {
		t.Fatalf("extended candidate patch = %#v", patch)
	}
}

func TestPrepareCandidateArtworkUsesRegistryReference(t *testing.T) {
	registry := providers.NewRegistry(artworkTestStrategy{})
	result, err := registry.Search(context.Background(), providers.Query{Title: "Song"}, nil, 1)
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("search = %#v err=%v", result, err)
	}
	want := artwork.Asset{MIME: "image/png", Data: []byte("image")}
	got, err := prepareCandidateArtwork(context.Background(), registry, result.Candidates[0], func(_ context.Context, reference providers.ArtworkReference) (artwork.Asset, error) {
		if reference.ProviderID != "artwork-test" || reference.URL == "" {
			t.Fatalf("reference = %#v", reference)
		}
		return want, nil
	})
	if err != nil || got == nil || string(got.Data) != "image" {
		t.Fatalf("asset = %#v err=%v", got, err)
	}
	_, err = prepareCandidateArtwork(context.Background(), registry, providers.MatchCandidate{ID: "missing"}, func(context.Context, providers.ArtworkReference) (artwork.Asset, error) {
		return want, nil
	})
	if !errors.Is(err, providers.ErrArtworkReferenceNotFound) {
		t.Fatalf("missing reference error = %v", err)
	}
}

func TestPatchFromBatchEditBuildsExplicitOperations(t *testing.T) {
	track := domain.Track{Album: "旧专辑", AlbumArtists: []string{"原艺人"}, Genres: []string{"Pop"}, Year: ptr(2020)}
	patch := patchFromBatchEdit(track, []domain.BatchEditOperation{
		{Field: "album", Mode: domain.BatchEditSet, Value: "新专辑"},
		{Field: "albumArtists", Mode: domain.BatchEditAppend, Value: "制作人"},
		{Field: "genres", Mode: domain.BatchEditAppend, Value: "Live, Pop"},
	}, true, 2, 5)
	if patch.Album == nil || patch.Album.Value != "新专辑" || patch.AlbumArtists == nil || len(patch.AlbumArtists.Value) != 2 ||
		patch.Genres == nil || len(patch.Genres.Value) != 2 || patch.TrackNumber == nil || patch.TrackNumber.Value != 3 || patch.TrackTotal == nil || patch.TrackTotal.Value != 5 {
		t.Fatalf("batch patch = %#v", patch)
	}
	deleted := patchFromBatchEdit(track, []domain.BatchEditOperation{
		{Field: "genres", Mode: domain.BatchEditDelete},
		{Field: "year", Mode: domain.BatchEditDelete},
	}, false, 0, 1)
	if deleted.Genres == nil || deleted.Genres.Op != domain.OperationDelete || deleted.Year == nil || deleted.Year.Op != domain.OperationDelete {
		t.Fatalf("delete batch patch = %#v", deleted)
	}
}

func ptr(value int) *int { return &value }
