package providers

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
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

type concurrentCountingStrategy struct {
	descriptor Descriptor
	calls      atomic.Int32
	delay      time.Duration
}

type barrierStrategy struct {
	descriptor Descriptor
	entered    chan<- string
	release    <-chan struct{}
}

type configurableStrategy struct {
	config map[string]string
}

type resettableConfigStrategy struct {
	config map[string]string
}

type artworkOptionsStrategy struct {
	fakeStrategy
	archiveDownloadBaseURL string
}

func (strategy artworkOptionsStrategy) ArtworkDownloadOptions() ArtworkDownloadOptions {
	return ArtworkDownloadOptions{ArchiveDownloadBaseURL: strategy.archiveDownloadBaseURL}
}

func (strategy *configurableStrategy) Descriptor() Descriptor {
	return Descriptor{ID: "configurable", Name: "Configurable", Enabled: true, Health: HealthReady}
}

func (strategy *configurableStrategy) Search(context.Context, Query, int) ([]Candidate, error) {
	return []Candidate{{ProviderID: "configurable", ExternalID: strategy.config["baseUrl"], Title: "Song"}}, nil
}

func (strategy *configurableStrategy) ConfigFields() []ConfigField {
	return []ConfigField{{Key: "baseUrl", Label: "Base URL", Type: "url", Value: strategy.config["baseUrl"]}, {Key: "auth", Label: "Auth", Type: "password", Value: strategy.config["auth"], Secret: true}}
}

func (strategy *configurableStrategy) Configure(values map[string]string) error {
	if strategy.config == nil {
		strategy.config = make(map[string]string)
	}
	for key, value := range values {
		strategy.config[key] = value
	}
	return nil
}

func (strategy *resettableConfigStrategy) Descriptor() Descriptor {
	return Descriptor{ID: "resettable", Name: "Resettable", Enabled: true, Health: HealthReady}
}

func (strategy *resettableConfigStrategy) Search(context.Context, Query, int) ([]Candidate, error) {
	return nil, nil
}

func (strategy *resettableConfigStrategy) ConfigFields() []ConfigField {
	return []ConfigField{
		{Key: "baseUrl", Label: "Base URL", Type: "url", Value: strategy.config["baseUrl"]},
		{Key: "userAgent", Label: "User-Agent", Type: "text", Value: strategy.config["userAgent"]},
		{Key: "auth", Label: "Auth", Type: "password", Value: strategy.config["auth"], Secret: true},
	}
}

func (strategy *resettableConfigStrategy) Configure(values map[string]string) error {
	if strategy.config == nil {
		strategy.config = make(map[string]string)
	}
	for key, value := range values {
		strategy.config[key] = value
	}
	return nil
}

func (strategy *resettableConfigStrategy) ResetConfig() error {
	strategy.config = map[string]string{
		"baseUrl":   "https://reset.example.test",
		"userAgent": BrowserUserAgent,
	}
	return nil
}

func (strategy *countingStrategy) Descriptor() Descriptor { return strategy.descriptor }
func (strategy *countingStrategy) Search(context.Context, Query, int) ([]Candidate, error) {
	strategy.calls++
	return []Candidate{{ExternalID: "1", Title: "Song", Artists: []string{"Artist"}, ArtworkURL: "https://is1-ssl.mzstatic.com/cover.jpg"}}, nil
}

func (strategy *concurrentCountingStrategy) Descriptor() Descriptor { return strategy.descriptor }
func (strategy *concurrentCountingStrategy) Search(context.Context, Query, int) ([]Candidate, error) {
	strategy.calls.Add(1)
	time.Sleep(strategy.delay)
	return []Candidate{{ExternalID: "1", Title: "Song", Artists: []string{"Artist"}}}, nil
}

func (strategy barrierStrategy) Descriptor() Descriptor { return strategy.descriptor }
func (strategy barrierStrategy) Search(ctx context.Context, _ Query, _ int) ([]Candidate, error) {
	select {
	case strategy.entered <- strategy.descriptor.ID:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-strategy.release:
		return []Candidate{{ExternalID: strategy.descriptor.ID, Title: "Song"}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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

func TestRegistryStartsSelectedProvidersInParallel(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	registry := NewRegistry(
		barrierStrategy{descriptor: Descriptor{ID: "source-a", Name: "A", Enabled: true, Health: HealthReady}, entered: entered, release: release},
		barrierStrategy{descriptor: Descriptor{ID: "source-b", Name: "B", Enabled: true, Health: HealthReady}, entered: entered, release: release},
	)
	done := make(chan error, 1)
	go func() {
		result, err := registry.Search(context.Background(), Query{Title: "Song"}, nil, 1)
		if err == nil && len(result.Providers) != 2 {
			err = errors.New("parallel search did not return both providers")
		}
		done <- err
	}()

	seen := map[string]bool{}
	for range 2 {
		select {
		case id := <-entered:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatal("providers did not both enter search before either was released")
		}
	}
	if !seen["source-a"] || !seen["source-b"] {
		t.Fatalf("entered providers = %v", seen)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRegistryUsesStrategyArtworkDownloadOptions(t *testing.T) {
	registry := NewRegistry(artworkOptionsStrategy{
		fakeStrategy:           fakeStrategy{descriptor: Descriptor{ID: "musicbrainz", Name: "MusicBrainz", Enabled: true, Health: HealthReady}},
		archiveDownloadBaseURL: "https://mirror.example.test",
	})
	options := registry.artworkDownloadOptions("musicbrainz")
	if options.ArchiveDownloadBaseURL != "https://mirror.example.test" {
		t.Fatalf("artwork download options = %#v", options)
	}
	if missing := registry.artworkDownloadOptions("missing"); missing.ArchiveDownloadBaseURL != "" {
		t.Fatalf("missing provider options = %#v", missing)
	}
}

func TestTrackQueriesSearchAmbiguousHintsWithoutPromotingThem(t *testing.T) {
	track := domain.Track{
		DurationSeconds: 252,
		TagHints: []domain.TagHint{
			{Title: "宮崎歩", Artists: []string{"brave heart"}, Source: "filename", Pattern: "title-artist"},
			{Title: "brave heart", Artists: []string{"宮崎歩"}, Source: "filename", Pattern: "artist-title"},
		},
	}
	queries := TrackQueries(track, Query{})
	if len(queries) != 2 || queries[0].Title != "宮崎歩" || queries[1].Title != "brave heart" || queries[1].Artists[0] != "宮崎歩" {
		t.Fatalf("queries = %#v", queries)
	}
	if track.Title != "" || len(track.Artists) != 0 {
		t.Fatalf("hints mutated embedded fields: %#v", track)
	}
	strategy := &countingStrategy{descriptor: Descriptor{ID: "hint-source", Name: "Hint Source", Enabled: true, Health: HealthReady}}
	registry := NewRegistry(strategy)
	result, err := registry.SearchTrack(context.Background(), track, Query{}, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if strategy.calls != 2 || len(result.Candidates) != 1 {
		t.Fatalf("calls=%d candidates=%#v", strategy.calls, result.Candidates)
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

func TestAlternateTitlesImproveCandidateScore(t *testing.T) {
	registry := NewRegistry(fakeStrategy{descriptor: Descriptor{ID: "alias", Name: "Alias", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
		ExternalID: "alias-1", Title: "现场版", AlternateTitles: []string{"原曲名"}, Artists: []string{"歌手"},
		ArtworkURL: "https://images.example.test/cover.jpg",
	}}})
	result, err := registry.Search(context.Background(), Query{Title: "原曲名", Artists: []string{"歌手"}}, []string{"alias"}, 1)
	if err != nil || len(result.Candidates) != 1 || result.Candidates[0].Score < 0.9 {
		t.Fatalf("alias score = %#v err=%v", result, err)
	}
}

func TestRegistryRanksCandidatesWithArtworkAheadOfMetadataOnlyMatches(t *testing.T) {
	registry := NewRegistry(fakeStrategy{descriptor: Descriptor{ID: "source", Name: "Source", Enabled: true, Health: HealthReady}, candidates: []Candidate{
		{ExternalID: "no-artwork", Title: "最佳歌手", Artists: []string{"许嵩"}},
		{ExternalID: "with-artwork", Title: "最佳歌手", Artists: []string{"许嵩"}, ArtworkURL: "https://images.example.test/cover.jpg"},
	}})
	result, err := registry.Search(context.Background(), Query{Title: "最佳歌手", Artists: []string{"许嵩"}}, []string{"source"}, 5)
	if err != nil || len(result.Candidates) != 2 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if result.Candidates[0].ExternalID != "with-artwork" {
		t.Fatalf("candidate order = %#v", result.Candidates)
	}
	if result.Candidates[1].Score > noArtworkConfidenceCap || result.Candidates[1].ScoreLabel == "高度匹配" {
		t.Fatalf("metadata-only candidate retained high confidence: %#v", result.Candidates[1])
	}
	if !containsString(result.Candidates[1].MatchReasons, "来源未提供封面") {
		t.Fatalf("metadata-only reasons = %#v", result.Candidates[1].MatchReasons)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func TestRegistryCoalescesConcurrentColdSearches(t *testing.T) {
	strategy := &concurrentCountingStrategy{descriptor: Descriptor{ID: "concurrent", Name: "Concurrent", Enabled: true, Health: HealthReady}, delay: 80 * time.Millisecond}
	registry := NewRegistry(strategy)
	query := Query{Title: "Song", Artists: []string{"Artist"}}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			result, err := registry.Search(context.Background(), query, []string{"concurrent"}, 5)
			if err == nil && len(result.Candidates) != 1 {
				err = errors.New("missing coalesced candidate")
			}
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls := strategy.calls.Load(); calls != 1 {
		t.Fatalf("strategy calls=%d, want one cold request", calls)
	}
}

func TestRegistryBypassCacheForcesLiveSearchAndRefreshesCache(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	strategy := &countingStrategy{descriptor: Descriptor{ID: "live", Name: "Live", Enabled: true, Health: HealthReady}}
	registry := NewRegistry(strategy)
	if err := registry.SetPersistence(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	query := Query{Title: "Song", Artists: []string{"Artist"}}
	if _, err := registry.Search(context.Background(), query, []string{"live"}, 1); err != nil {
		t.Fatal(err)
	}
	cached, err := registry.Search(context.Background(), query, []string{"live"}, 1)
	if err != nil || !cached.Providers["live"].Cached || strategy.calls != 1 {
		t.Fatalf("cached=%#v calls=%d err=%v", cached.Providers, strategy.calls, err)
	}
	live, err := registry.SearchWithOptions(context.Background(), query, []string{"live"}, 1, SearchOptions{BypassCache: true})
	if err != nil || live.Providers["live"].Cached || strategy.calls != 2 {
		t.Fatalf("live=%#v calls=%d err=%v", live.Providers, strategy.calls, err)
	}
	refreshed, err := registry.Search(context.Background(), query, []string{"live"}, 1)
	if err != nil || !refreshed.Providers["live"].Cached || strategy.calls != 2 {
		t.Fatalf("refreshed=%#v calls=%d err=%v", refreshed.Providers, strategy.calls, err)
	}
}

func TestRegistryRejectsEnablingUnavailableExperimentalProvider(t *testing.T) {
	registry := NewRegistry(NewPlaceholder(Descriptor{ID: "netease", Enabled: false, Experimental: true, Health: HealthDisabled}))
	_, err := registry.SetEnabled(context.Background(), "netease", true)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestRegistryAppliesAndMasksProviderConfiguration(t *testing.T) {
	strategy := &configurableStrategy{config: map[string]string{"baseUrl": "https://initial.test", "auth": "secret"}}
	registry := NewRegistry(strategy)
	descriptor, err := registry.SetConfig(context.Background(), "configurable", map[string]string{"baseUrl": "https://updated.test", "auth": "new-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptor.Config) != 2 || descriptor.Config[0].Value != "https://updated.test" || descriptor.Config[1].Value != "" || !descriptor.Config[1].Configured {
		t.Fatalf("descriptor config = %#v", descriptor.Config)
	}
	if strategy.config["baseUrl"] != "https://updated.test" || strategy.config["auth"] != "new-secret" {
		t.Fatalf("strategy config = %#v", strategy.config)
	}
}

func TestRegistryResetsProviderConfigurationAndPersistsDefaultUA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagger.db")
	repository, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	strategy := &resettableConfigStrategy{config: map[string]string{"baseUrl": "https://initial.example.test", "userAgent": "Mozilla/5.0", "auth": "secret"}}
	registry := NewRegistry(strategy)
	if err := registry.SetPersistence(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetConfig(context.Background(), "resettable", map[string]string{"baseUrl": "https://custom.example.test", "userAgent": "Tagger/legacy", "auth": "secret"}); err != nil {
		t.Fatal(err)
	}
	descriptor, err := registry.ResetConfig(context.Background(), "resettable")
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Config[0].Value != "https://reset.example.test" || descriptor.Config[1].Value != BrowserUserAgent || descriptor.Config[2].Configured {
		t.Fatalf("reset descriptor = %#v", descriptor.Config)
	}
	configurations, err := repository.LoadProviderConfigurations(context.Background())
	if err != nil || len(configurations["resettable"]) != 0 {
		t.Fatalf("persisted reset configuration = %#v err=%v", configurations, err)
	}
}

func TestRegistryMigratesLegacyBuiltInUserAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagger.db")
	repository, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := repository.SaveProviderConfiguration(context.Background(), "lrclib", map[string]string{"userAgent": "Tagger/dev (https://github.com/ericwyn/tagger)", "baseUrl": "https://custom.example.test"}); err != nil {
		t.Fatal(err)
	}
	strategy := &resettableConfigStrategy{config: map[string]string{}}
	// The fake deliberately uses the built-in ID so the registry migration
	// exercises the same persisted key used by the real LRCLIB strategy.
	strategyDescriptor := strategy.Descriptor()
	strategyDescriptor.ID = "lrclib"
	strategyDescriptor.Name = "LRCLIB"
	registry := NewRegistry(&configurableAliasStrategy{resettableConfigStrategy: strategy, descriptor: strategyDescriptor})
	if err := registry.SetPersistence(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	if got := strategy.config["userAgent"]; got != BrowserUserAgent {
		t.Fatalf("migrated user-agent = %q, want %q", got, BrowserUserAgent)
	}
	configurations, err := repository.LoadProviderConfigurations(context.Background())
	if err != nil || configurations["lrclib"]["userAgent"] != BrowserUserAgent {
		t.Fatalf("migrated persisted configuration = %#v err=%v", configurations, err)
	}
}

type configurableAliasStrategy struct {
	*resettableConfigStrategy
	descriptor Descriptor
}

func (strategy *configurableAliasStrategy) Descriptor() Descriptor { return strategy.descriptor }
