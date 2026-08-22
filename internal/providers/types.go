package providers

import (
	"context"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
)

type Health string

const (
	HealthReady         Health = "ready"
	HealthDegraded      Health = "degraded"
	HealthMisconfigured Health = "misconfigured"
	HealthDisabled      Health = "disabled"
)

type Descriptor struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ShortName    string   `json:"shortName"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
	Health       Health   `json:"health"`
	Enabled      bool     `json:"enabled"`
	Experimental bool     `json:"experimental,omitempty"`
	Accent       string   `json:"accent"`
	QuotaLabel   string   `json:"quotaLabel"`
}

type Query struct {
	Title           string
	Artists         []string
	Album           string
	DurationSeconds int64
}

type Candidate struct {
	ProviderID      string
	ExternalID      string
	Title           string
	Artists         []string
	Album           string
	AlbumArtists    []string
	Year            int
	TrackNumber     int
	TrackTotal      int
	DiscNumber      int
	DiscTotal       int
	DurationSeconds int64
	Genres          []string
	Lyrics          string
	SyncedLyrics    string
	ArtworkURL      string
}

type Strategy interface {
	Descriptor() Descriptor
	Search(ctx context.Context, query Query, limit int) ([]Candidate, error)
}

type Field[T any] struct {
	Value  T      `json:"value"`
	Source string `json:"source"`
}

type MatchCandidate struct {
	ID              string           `json:"id"`
	ProviderID      string           `json:"providerId"`
	ProviderName    string           `json:"providerName"`
	ExternalID      string           `json:"externalId"`
	Title           Field[string]    `json:"title"`
	Artists         Field[[]string]  `json:"artists"`
	Album           Field[string]    `json:"album"`
	AlbumArtists    Field[[]string]  `json:"albumArtists"`
	Year            Field[int]       `json:"year"`
	TrackNumber     Field[int]       `json:"trackNumber"`
	TrackTotal      Field[int]       `json:"trackTotal"`
	DiscNumber      Field[int]       `json:"discNumber"`
	DurationSeconds Field[int64]     `json:"durationSeconds"`
	Genres          Field[[]string]  `json:"genres"`
	Lyrics          *Field[string]   `json:"lyrics,omitempty"`
	HasLyrics       bool             `json:"hasLyrics"`
	HasArtwork      bool             `json:"hasArtwork"`
	CoverTone       domain.CoverTone `json:"coverTone"`
	Score           float64          `json:"score"`
	ScoreLabel      string           `json:"scoreLabel"`
	MatchReasons    []string         `json:"matchReasons"`
}

type ProviderResult struct {
	Status    string `json:"status"`
	Count     int    `json:"count"`
	LatencyMS int64  `json:"latencyMs"`
	Retryable bool   `json:"retryable,omitempty"`
	Error     string `json:"error,omitempty"`
	Cached    bool   `json:"cached,omitempty"`
}

type SearchResult struct {
	Candidates []MatchCandidate          `json:"candidates"`
	Providers  map[string]ProviderResult `json:"providers"`
}

type searchOutcome struct {
	descriptor Descriptor
	candidates []Candidate
	err        error
	duration   time.Duration
	cached     bool
}
