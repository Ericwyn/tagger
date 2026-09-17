package providers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
)

// DefaultBatchCandidateLimit keeps automatic matching fast while retaining a
// second result for version/disambiguation checks. Interactive searches can
// continue to request a wider result set.
const DefaultBatchCandidateLimit = 2

func ParseRateInterval(value string) (time.Duration, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 || parsed > 60000 {
		return 0, fmt.Errorf("rateIntervalMs 必须是 0 到 60000 之间的整数")
	}
	return time.Duration(parsed) * time.Millisecond, nil
}

func ValidateHTTPURL(value, field string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("%s 必须是 HTTP(S) URL", field)
	}
	return strings.TrimSpace(parsed.String()), nil
}

type Health string

const (
	HealthReady         Health = "ready"
	HealthDegraded      Health = "degraded"
	HealthMisconfigured Health = "misconfigured"
	HealthDisabled      Health = "disabled"
)

// ConfigField describes one strategy-owned runtime setting. Values are kept
// on the strategy and returned to the UI only after the registry masks secret
// fields. A strategy may choose a text, password or number input type.
type ConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Value       string `json:"value,omitempty"`
	Configured  bool   `json:"configured,omitempty"`
}

// Configurable is implemented by providers that can apply their endpoint,
// credential and transport settings without rebuilding the process.
type Configurable interface {
	ConfigFields() []ConfigField
	Configure(map[string]string) error
}

// ConfigResetter is implemented by built-in providers that can restore their
// constructor defaults at runtime. It is intentionally separate from
// Configurable so third-party strategies written against the older interface
// remain loadable; the UI simply disables reset for strategies that do not
// expose this optional capability.
type ConfigResetter interface {
	ResetConfig() error
}

type Descriptor struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	ShortName    string        `json:"shortName"`
	Description  string        `json:"description"`
	Capabilities []string      `json:"capabilities"`
	Health       Health        `json:"health"`
	Enabled      bool          `json:"enabled"`
	Experimental bool          `json:"experimental,omitempty"`
	Accent       string        `json:"accent"`
	QuotaLabel   string        `json:"quotaLabel"`
	Config       []ConfigField `json:"config,omitempty"`
	ConfigError  string        `json:"configError,omitempty"`
}

type Query struct {
	Title                string
	Artists              []string
	Album                string
	AlbumArtists         []string
	Year                 int
	TrackNumber          int
	DiscNumber           int
	DurationSeconds      int64
	ISRC                 string
	MusicBrainzTrackID   string
	MusicBrainzReleaseID string
	AcoustID             string
	AcoustIDFingerprint  string
}

type Candidate struct {
	ProviderID string
	ExternalID string
	Title      string
	// AlternateTitles contains aliases returned by a provider. They are kept
	// internal to matching so an alias such as "Song (Live)" can still score
	// well without changing the title that will be written to the file.
	AlternateTitles      []string
	Artists              []string
	Album                string
	AlbumArtists         []string
	Year                 int
	TrackNumber          int
	TrackTotal           int
	DiscNumber           int
	DiscTotal            int
	DurationSeconds      int64
	Genres               []string
	Lyrics               string
	SyncedLyrics         string
	ArtworkURL           string
	Comment              string
	Composers            []string
	Conductor            string
	Lyricists            []string
	Copyright            string
	BPM                  int
	ISRC                 string
	MusicBrainzTrackID   string
	MusicBrainzReleaseID string
	MusicBrainzArtistIDs []string
	AcoustID             string
	AcoustIDFingerprint  string
}

type Strategy interface {
	Descriptor() Descriptor
	Search(ctx context.Context, query Query, limit int) ([]Candidate, error)
}

type CandidateKind string

const (
	CandidateKindSource CandidateKind = "source"
	CandidateKindSmart  CandidateKind = "smart"
	CandidateKindAI     CandidateKind = "ai"
)

// SourceReference keeps a composite field auditable without exposing the
// provider's private artwork URL or requiring the UI to reverse-engineer a
// candidate ID. Multiple references mean independent candidates confirmed the
// same normalized value; the first item is the value that was retained.
type SourceReference struct {
	ProviderID   string `json:"providerId"`
	ProviderName string `json:"providerName"`
	CandidateID  string `json:"candidateId"`
	ExternalID   string `json:"externalId,omitempty"`
}

type Field[T any] struct {
	Value      T                 `json:"value"`
	Source     string            `json:"source"`
	Sources    []SourceReference `json:"sources,omitempty"`
	Confidence float64           `json:"confidence,omitempty"`
	Derived    bool              `json:"derived,omitempty"`
}

// MatchEvidence deliberately separates identity from metadata completeness
// and asset availability. A missing cover must never make an otherwise exact
// recording match look less certain.
type MatchEvidence struct {
	IdentityScore     float64  `json:"identityScore"`
	ReleaseScore      float64  `json:"releaseScore,omitempty"`
	CompletenessScore float64  `json:"completenessScore,omitempty"`
	AssetQuality      float64  `json:"assetQuality,omitempty"`
	Margin            float64  `json:"margin,omitempty"`
	Level             string   `json:"level"`
	SourceCount       int      `json:"sourceCount"`
	Conflicts         []string `json:"conflicts,omitempty"`
	AlgorithmVersion  string   `json:"algorithmVersion,omitempty"`
}

type MatchCandidate struct {
	ID                   string            `json:"id"`
	Kind                 CandidateKind     `json:"kind,omitempty"`
	ProviderID           string            `json:"providerId"`
	ProviderName         string            `json:"providerName"`
	ExternalID           string            `json:"externalId"`
	MemberCandidateIDs   []string          `json:"memberCandidateIds,omitempty"`
	Contributors         []SourceReference `json:"contributors,omitempty"`
	ArtworkReferenceID   string            `json:"artworkRefId,omitempty"`
	ArtworkSource        *SourceReference  `json:"artworkSource,omitempty"`
	Title                Field[string]     `json:"title"`
	Artists              Field[[]string]   `json:"artists"`
	Album                Field[string]     `json:"album"`
	AlbumArtists         Field[[]string]   `json:"albumArtists"`
	Year                 Field[int]        `json:"year"`
	TrackNumber          Field[int]        `json:"trackNumber"`
	TrackTotal           Field[int]        `json:"trackTotal"`
	DiscNumber           Field[int]        `json:"discNumber"`
	DiscTotal            Field[int]        `json:"discTotal"`
	DurationSeconds      Field[int64]      `json:"durationSeconds"`
	Genres               Field[[]string]   `json:"genres"`
	Comment              Field[string]     `json:"comment"`
	Composers            Field[[]string]   `json:"composers"`
	Conductor            Field[string]     `json:"conductor"`
	Lyricists            Field[[]string]   `json:"lyricists"`
	Copyright            Field[string]     `json:"copyright"`
	BPM                  Field[int]        `json:"bpm"`
	ISRC                 Field[string]     `json:"isrc"`
	MusicBrainzTrackID   Field[string]     `json:"musicbrainzTrackId"`
	MusicBrainzReleaseID Field[string]     `json:"musicbrainzReleaseId"`
	MusicBrainzArtistIDs Field[[]string]   `json:"musicbrainzArtistIds"`
	AcoustID             Field[string]     `json:"acoustidId"`
	AcoustIDFingerprint  Field[string]     `json:"acoustidFingerprint"`
	Lyrics               *Field[string]    `json:"lyrics,omitempty"`
	HasLyrics            bool              `json:"hasLyrics"`
	HasArtwork           bool              `json:"hasArtwork"`
	CoverTone            domain.CoverTone  `json:"coverTone"`
	Score                float64           `json:"score"`
	ScoreLabel           string            `json:"scoreLabel"`
	MatchReasons         []string          `json:"matchReasons"`
	Evidence             MatchEvidence     `json:"evidence"`
	Recommended          bool              `json:"recommended,omitempty"`
	AutoAccept           bool              `json:"autoAccept,omitempty"`
}

type ProviderResult struct {
	Status       string `json:"status"`
	Count        int    `json:"count"`
	LatencyMS    int64  `json:"latencyMs"`
	Retryable    bool   `json:"retryable,omitempty"`
	RetryAfterMS int64  `json:"retryAfterMs,omitempty"`
	Hint         string `json:"hint,omitempty"`
	Error        string `json:"error,omitempty"`
	Cached       bool   `json:"cached,omitempty"`
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

type providerSearchPayload struct {
	candidates []Candidate
	cached     bool
}
