package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/ericwyn/tagger/internal/providers"
)

type providerDiagnosticOptions struct {
	Limit        int
	ProbeArtwork bool
}

type providerDiagnosticReport struct {
	Query      providerDiagnosticQuery    `json:"query"`
	DurationMS int64                      `json:"durationMs"`
	Summary    providerDiagnosticSummary  `json:"summary"`
	Providers  []providerDiagnosticResult `json:"providers"`
}

type providerDiagnosticQuery struct {
	Title           string   `json:"title"`
	Artists         []string `json:"artists,omitempty"`
	Album           string   `json:"album,omitempty"`
	DurationSeconds int64    `json:"durationSeconds,omitempty"`
	Limit           int      `json:"limit"`
	ProbeArtwork    bool     `json:"probeArtwork"`
}

type providerDiagnosticSummary struct {
	Passed   int `json:"passed"`
	Warnings int `json:"warnings"`
	Failed   int `json:"failed"`
}

type providerDiagnosticResult struct {
	ID             string                        `json:"id"`
	Name           string                        `json:"name"`
	Enabled        bool                          `json:"enabled"`
	Status         string                        `json:"status"`
	CandidateCount int                           `json:"candidateCount"`
	LyricsCount    int                           `json:"lyricsCount"`
	ArtworkCount   int                           `json:"artworkCount"`
	ArtworkPassed  int                           `json:"artworkPassed"`
	ArtworkFailed  int                           `json:"artworkFailed"`
	LatencyMS      int64                         `json:"latencyMs"`
	Error          string                        `json:"error,omitempty"`
	Hint           string                        `json:"hint,omitempty"`
	Notes          []string                      `json:"notes,omitempty"`
	Candidates     []providerDiagnosticCandidate `json:"candidates"`
}

type providerDiagnosticCandidate struct {
	ID           string   `json:"id"`
	ExternalID   string   `json:"externalId"`
	Title        string   `json:"title"`
	Artists      []string `json:"artists,omitempty"`
	Album        string   `json:"album,omitempty"`
	Score        float64  `json:"score"`
	LyricsChars  int      `json:"lyricsChars"`
	HasArtwork   bool     `json:"hasArtwork"`
	ArtworkProbe string   `json:"artworkProbe,omitempty"`
	ArtworkError string   `json:"artworkError,omitempty"`
}

func executeProviderDiagnostics(ctx context.Context, registry *providers.Registry, query providers.Query, options providerDiagnosticOptions, download artworkDownloader) (providerDiagnosticReport, error) {
	started := time.Now()
	result, err := registry.SearchWithOptions(ctx, query, nil, options.Limit, providers.SearchOptions{BypassCache: true, IncludeDisabled: true})
	if err != nil {
		return providerDiagnosticReport{}, err
	}
	report := providerDiagnosticReport{
		Query: providerDiagnosticQuery{
			Title: query.Title, Artists: query.Artists, Album: query.Album, DurationSeconds: query.DurationSeconds,
			Limit: options.Limit, ProbeArtwork: options.ProbeArtwork,
		},
		Providers: make([]providerDiagnosticResult, 0, len(result.Providers)),
	}
	candidatesByProvider := make(map[string][]providers.MatchCandidate)
	for _, candidate := range result.Candidates {
		candidatesByProvider[candidate.ProviderID] = append(candidatesByProvider[candidate.ProviderID], candidate)
	}
	for _, descriptor := range registry.Descriptors() {
		outcome, tested := result.Providers[descriptor.ID]
		if !tested {
			continue
		}
		item := providerDiagnosticResult{
			ID: descriptor.ID, Name: descriptor.Name, Enabled: descriptor.Enabled,
			Status: "pass", CandidateCount: outcome.Count, LatencyMS: outcome.LatencyMS,
			Error: outcome.Error, Hint: outcome.Hint,
			Candidates: make([]providerDiagnosticCandidate, 0, len(candidatesByProvider[descriptor.ID])),
		}
		if outcome.Status != "ok" {
			item.Status = "fail"
		} else if outcome.Count == 0 {
			item.Status = "warning"
			item.Notes = append(item.Notes, "no candidates returned")
		}
		for _, candidate := range candidatesByProvider[descriptor.ID] {
			candidateResult := providerDiagnosticCandidate{
				ID: candidate.ID, ExternalID: candidate.ExternalID, Title: candidate.Title.Value,
				Artists: candidate.Artists.Value, Album: candidate.Album.Value, Score: candidate.Score,
				HasArtwork: candidate.HasArtwork,
			}
			if candidate.Lyrics != nil {
				candidateResult.LyricsChars = utf8.RuneCountInString(candidate.Lyrics.Value)
			}
			if candidateResult.LyricsChars > 0 {
				item.LyricsCount++
			}
			if candidate.HasArtwork {
				item.ArtworkCount++
				candidateResult.ArtworkProbe = "not-probed"
			}
			if options.ProbeArtwork && candidate.HasArtwork {
				reference, referenceErr := registry.ArtworkReference(candidate.ID)
				if referenceErr != nil {
					candidateResult.ArtworkProbe = "failed"
					candidateResult.ArtworkError = referenceErr.Error()
					item.ArtworkFailed++
					item.Status = "fail"
				} else if download == nil {
					candidateResult.ArtworkProbe = "failed"
					candidateResult.ArtworkError = "artwork downloader is unavailable"
					item.ArtworkFailed++
					item.Status = "fail"
				} else if asset, artworkErr := download(ctx, reference); artworkErr != nil {
					candidateResult.ArtworkProbe = "failed"
					candidateResult.ArtworkError = artworkErr.Error()
					item.ArtworkFailed++
					item.Status = "fail"
				} else {
					candidateResult.ArtworkProbe = fmt.Sprintf("ok %dx%d %s", asset.Width, asset.Height, asset.MIME)
					item.ArtworkPassed++
				}
			}
			item.Candidates = append(item.Candidates, candidateResult)
		}
		if item.Status == "pass" && slices.Contains(descriptor.Capabilities, "歌词") && item.LyricsCount == 0 {
			item.Status = "warning"
			item.Notes = append(item.Notes, "no candidate contained lyrics")
		}
		if item.Status == "pass" && slices.Contains(descriptor.Capabilities, "封面") && item.ArtworkCount == 0 {
			item.Status = "warning"
			item.Notes = append(item.Notes, "no candidate contained artwork")
		}
		switch item.Status {
		case "fail":
			report.Summary.Failed++
		case "warning":
			report.Summary.Warnings++
		default:
			report.Summary.Passed++
		}
		report.Providers = append(report.Providers, item)
	}
	report.DurationMS = time.Since(started).Milliseconds()
	return report, nil
}

func writeProviderDiagnostics(output io.Writer, report providerDiagnosticReport, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Fprintf(output, "Provider diagnostics: title=%q artists=%q limit=%d artwork=%t\n\n", report.Query.Title, strings.Join(report.Query.Artists, ", "), report.Query.Limit, report.Query.ProbeArtwork)
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "SOURCE\tCONFIG\tSTATE\tCANDIDATES\tLYRICS\tARTWORK\tPROBE\tLATENCY")
	for _, item := range report.Providers {
		probe := "-"
		if report.Query.ProbeArtwork && item.ArtworkCount > 0 {
			probe = fmt.Sprintf("%d/%d", item.ArtworkPassed, item.ArtworkCount)
		}
		configured := "disabled"
		if item.Enabled {
			configured = "enabled"
		}
		fmt.Fprintf(writer, "%s (%s)\t%s\t%s\t%d\t%d\t%d\t%s\t%dms\n", item.Name, item.ID, configured, strings.ToUpper(item.Status), item.CandidateCount, item.LyricsCount, item.ArtworkCount, probe, item.LatencyMS)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	for _, item := range report.Providers {
		if item.Error == "" && len(item.Notes) == 0 && len(item.Candidates) == 0 {
			continue
		}
		fmt.Fprintf(output, "\n[%s] %s\n", item.ID, item.Name)
		if item.Error != "" {
			fmt.Fprintf(output, "  error: %s\n", oneLine(item.Error))
			if item.Hint != "" {
				fmt.Fprintf(output, "  hint: %s\n", oneLine(item.Hint))
			}
		}
		for _, note := range item.Notes {
			fmt.Fprintf(output, "  note: %s\n", note)
		}
		for _, candidate := range item.Candidates {
			assets := fmt.Sprintf("lyrics=%d artwork=%t", candidate.LyricsChars, candidate.HasArtwork)
			if candidate.ArtworkProbe != "" {
				assets += " probe=" + candidate.ArtworkProbe
			}
			fmt.Fprintf(output, "  - %s / %s | album=%s | score=%.3f | %s\n", oneLine(candidate.Title), oneLine(strings.Join(candidate.Artists, ", ")), oneLine(candidate.Album), candidate.Score, assets)
			if candidate.ArtworkError != "" {
				fmt.Fprintf(output, "    artwork error: %s\n", oneLine(candidate.ArtworkError))
			}
		}
	}
	fmt.Fprintf(output, "\nSummary: %d passed, %d warnings, %d failed (%dms)\n", report.Summary.Passed, report.Summary.Warnings, report.Summary.Failed, report.DurationMS)
	return nil
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func parseProviderTestArtists(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == '；' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
