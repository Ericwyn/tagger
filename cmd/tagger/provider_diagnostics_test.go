package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/providers"
)

type diagnosticStrategy struct {
	descriptor providers.Descriptor
	candidates []providers.Candidate
	err        error
}

func (strategy diagnosticStrategy) Descriptor() providers.Descriptor { return strategy.descriptor }
func (strategy diagnosticStrategy) Search(context.Context, providers.Query, int) ([]providers.Candidate, error) {
	return strategy.candidates, strategy.err
}

func TestExecuteProviderDiagnosticsChecksDisabledProviderAssets(t *testing.T) {
	registry := providers.NewRegistry(diagnosticStrategy{
		descriptor: providers.Descriptor{ID: "source", Name: "Source", Enabled: false, Health: providers.HealthDegraded, Capabilities: []string{"歌词", "封面"}},
		candidates: []providers.Candidate{{ExternalID: "external", Title: "Song", Artists: []string{"Artist"}, Lyrics: "[00:01.00]line", ArtworkURL: "https://images.example.test/cover.jpg"}},
	})
	report, err := executeProviderDiagnostics(context.Background(), registry, providers.Query{Title: "Song", Artists: []string{"Artist"}}, providerDiagnosticOptions{Limit: 1, ProbeArtwork: true}, func(_ context.Context, reference providers.ArtworkReference) (artwork.Asset, error) {
		if reference.ProviderID != "source" {
			t.Fatalf("reference = %#v", reference)
		}
		return artwork.Asset{MIME: "image/jpeg", Width: 500, Height: 500}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Passed != 1 || len(report.Providers) != 1 {
		t.Fatalf("report = %#v", report)
	}
	result := report.Providers[0]
	if result.Enabled || result.CandidateCount != 1 || result.LyricsCount != 1 || result.ArtworkPassed != 1 || result.Candidates[0].ArtworkProbe != "ok 500x500 image/jpeg" {
		t.Fatalf("result = %#v", result)
	}
	var human bytes.Buffer
	if err := writeProviderDiagnostics(&human, report, false); err != nil || !strings.Contains(human.String(), "Source (source)") || !strings.Contains(human.String(), "1 passed") {
		t.Fatalf("human output=%q err=%v", human.String(), err)
	}
	var jsonOutput bytes.Buffer
	if err := writeProviderDiagnostics(&jsonOutput, report, true); err != nil || !strings.Contains(jsonOutput.String(), `"artworkPassed": 1`) {
		t.Fatalf("json output=%q err=%v", jsonOutput.String(), err)
	}
}

func TestExecuteProviderDiagnosticsReportsSearchAndArtworkFailures(t *testing.T) {
	registry := providers.NewRegistry(
		diagnosticStrategy{descriptor: providers.Descriptor{ID: "search-fail", Name: "Search Fail", Enabled: true, Health: providers.HealthReady}, err: errors.New("upstream unavailable")},
		diagnosticStrategy{descriptor: providers.Descriptor{ID: "art-fail", Name: "Art Fail", Enabled: true, Health: providers.HealthReady}, candidates: []providers.Candidate{{ExternalID: "1", Title: "Song", ArtworkURL: "https://images.example.test/cover.jpg"}}},
	)
	report, err := executeProviderDiagnostics(context.Background(), registry, providers.Query{Title: "Song"}, providerDiagnosticOptions{Limit: 1, ProbeArtwork: true}, func(context.Context, providers.ArtworkReference) (artwork.Asset, error) {
		return artwork.Asset{}, errors.New("certificate mismatch")
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Failed != 2 || report.Providers[0].Status != "fail" || report.Providers[1].Status != "fail" {
		t.Fatalf("report = %#v", report)
	}
}

func TestParseProviderTestArtists(t *testing.T) {
	actual := parseProviderTestArtists(" Adele，阿黛尔; Guest ")
	if len(actual) != 3 || actual[0] != "Adele" || actual[1] != "阿黛尔" || actual[2] != "Guest" {
		t.Fatalf("artists = %#v", actual)
	}
}
