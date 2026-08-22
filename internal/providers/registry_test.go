package providers

import (
	"context"
	"errors"
	"testing"
)

type fakeStrategy struct {
	descriptor Descriptor
	candidates []Candidate
	err        error
}

func (f fakeStrategy) Descriptor() Descriptor { return f.descriptor }
func (f fakeStrategy) Search(context.Context, Query, int) ([]Candidate, error) {
	return f.candidates, f.err
}

func TestRegistryAggregatesScoresAndProviderFailures(t *testing.T) {
	registry := NewRegistry(
		fakeStrategy{descriptor: Descriptor{ID: "exact", Name: "Exact", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ProviderID: "exact", ExternalID: "1", Title: "再回首", Artists: []string{"姜育恒"}, Album: "多年以后", DurationSeconds: 255,
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
