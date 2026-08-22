package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ericwyn/tagger/internal/artwork"
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
