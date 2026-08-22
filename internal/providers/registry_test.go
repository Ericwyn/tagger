package providers

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ericwyn/tagger/internal/store"
)

type fakeStrategy struct {
	descriptor Descriptor
	candidates []Candidate
	err        error
}

type countingStrategy struct {
	descriptor Descriptor
	calls      int
}

func (strategy *countingStrategy) Descriptor() Descriptor { return strategy.descriptor }
func (strategy *countingStrategy) Search(context.Context, Query, int) ([]Candidate, error) {
	strategy.calls++
	return []Candidate{{ExternalID: "1", Title: "Song", Artists: []string{"Artist"}, ArtworkURL: "https://is1-ssl.mzstatic.com/cover.jpg"}}, nil
}

func (f fakeStrategy) Descriptor() Descriptor { return f.descriptor }
func (f fakeStrategy) Search(context.Context, Query, int) ([]Candidate, error) {
	return f.candidates, f.err
}

func TestRegistryAggregatesScoresAndProviderFailures(t *testing.T) {
	registry := NewRegistry(
		fakeStrategy{descriptor: Descriptor{ID: "exact", Name: "Exact", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ProviderID: "exact", ExternalID: "1", Title: "再回首", Artists: []string{"姜育恒"}, Album: "多年以后", DurationSeconds: 255,
			ArtworkURL: "https://images.example.test/cover.jpg",
		}}},
		fakeStrategy{descriptor: Descriptor{ID: "weak", Name: "Weak", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ProviderID: "weak", ExternalID: "2", Title: "别回首", Artists: []string{"其他人"}, DurationSeconds: 310,
		}}},
		fakeStrategy{descriptor: Descriptor{ID: "broken", Name: "Broken", Enabled: true, Health: HealthReady}, err: &HTTPError{Status: 503}},
	)
	result, err := registry.Search(context.Background(), Query{Title: "再回首", Artists: []string{"姜育恒"}, Album: "多年以后", DurationSeconds: 255}, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 || result.Candidates[0].ProviderID != "exact" || result.Candidates[0].Score < 0.95 {
		t.Fatalf("candidates = %#v", result.Candidates)
	}
	if result.Providers["broken"].Status != "error" || !result.Providers["broken"].Retryable {
		t.Fatalf("broken provider = %#v", result.Providers["broken"])
	}
	if result.Candidates[0].Artists.Value == nil || result.Candidates[0].Genres.Value == nil {
		t.Fatalf("multi-value fields must not be nil: %#v", result.Candidates[0])
	}
	reference, err := registry.ArtworkReference(result.Candidates[0].ID)
	if err != nil || reference.ProviderID != "exact" || reference.URL != "https://images.example.test/cover.jpg" {
		t.Fatalf("artwork reference = %#v err=%v", reference, err)
	}
}

func TestRegistryRejectsUnknownProvider(t *testing.T) {
	registry := NewRegistry()
	_, err := registry.Search(context.Background(), Query{Title: "Song"}, []string{"missing"}, 5)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestSimilarityHandlesPunctuationAndCJK(t *testing.T) {
	if got := similarity("我很丑，可是我很温柔", "我很丑可是我很温柔"); got != 1 {
		t.Fatalf("punctuation similarity = %f", got)
	}
	if got := similarity("AC/DC", "ACDC"); got != 1 {
		t.Fatalf("ASCII similarity = %f", got)
	}
}

func TestRegistryCacheAndSettingsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagger.db")
	repository, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	strategy := &countingStrategy{descriptor: Descriptor{ID: "apple", Name: "Apple", Enabled: true, Health: HealthReady}}
	registry := NewRegistry(strategy)
	if err := registry.SetPersistence(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	query := Query{Title: "Song", Artists: []string{"Artist"}}
	first, err := registry.Search(context.Background(), query, []string{"apple"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Search(context.Background(), query, []string{"apple"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if strategy.calls != 1 || first.Providers["apple"].Cached || !second.Providers["apple"].Cached {
		t.Fatalf("calls=%d first=%#v second=%#v", strategy.calls, first.Providers, second.Providers)
	}
	if _, err := registry.SetEnabled(context.Background(), "apple", false); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	secondStrategy := &countingStrategy{descriptor: strategy.descriptor}
	secondRegistry := NewRegistry(secondStrategy)
	if err := secondRegistry.SetPersistence(context.Background(), reopened); err != nil {
		t.Fatal(err)
	}
	if reference, err := secondRegistry.ArtworkReference(first.Candidates[0].ID); err != nil || reference.ProviderID != "apple" {
		t.Fatalf("reopened artwork reference = %#v err=%v", reference, err)
	}
	if descriptor, _ := secondRegistry.Descriptor("apple"); descriptor.Enabled || descriptor.Health != HealthDisabled {
		t.Fatalf("persisted descriptor = %#v", descriptor)
	}
	if _, err := secondRegistry.SetEnabled(context.Background(), "apple", true); err != nil {
		t.Fatal(err)
	}
	result, err := secondRegistry.Search(context.Background(), query, []string{"apple"}, 5)
	if err != nil || !result.Providers["apple"].Cached || secondStrategy.calls != 0 {
		t.Fatalf("reopened cache=%#v calls=%d err=%v", result, secondStrategy.calls, err)
	}
	if _, err := secondRegistry.ArtworkReference(result.Candidates[0].ID); err != nil {
		t.Fatalf("cached artwork reference: %v", err)
	}
}

func TestRegistryRejectsEnablingUnavailableExperimentalProvider(t *testing.T) {
	registry := NewRegistry(NewPlaceholder(Descriptor{ID: "netease", Enabled: false, Experimental: true, Health: HealthDisabled}))
	_, err := registry.SetEnabled(context.Background(), "netease", true)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("error = %v", err)
	}
}
