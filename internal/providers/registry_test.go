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

func TestSearchTrackBuildsAuditableSmartCandidateAcrossAllSources(t *testing.T) {
	registry := NewRegistry(
		fakeStrategy{descriptor: Descriptor{ID: "metadata", Name: "Metadata", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ProviderID: "metadata", ExternalID: "meta-1", Title: "Song", Artists: []string{"Artist"}, Album: "Original Album",
			Year: 2024, TrackNumber: 2, DurationSeconds: 240, Genres: []string{"Pop"}, ArtworkURL: "https://images.example.test/song.jpg",
		}}},
		fakeStrategy{descriptor: Descriptor{ID: "lyrics", Name: "Lyrics", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ProviderID: "lyrics", ExternalID: "lyrics-1", Title: "Song", Artists: []string{"Artist"}, Album: "Original Album",
			DurationSeconds: 240, SyncedLyrics: "[00:01.00]line one\n[00:05.00]line two",
		}}},
		fakeStrategy{descriptor: Descriptor{ID: "catalog", Name: "Catalog", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ProviderID: "catalog", ExternalID: "catalog-1", Title: "Song", Artists: []string{"Artist"}, Album: "Original Album",
			DurationSeconds: 240, ISRC: "US-AAA-24-00001", MusicBrainzTrackID: "recording-1", MusicBrainzReleaseID: "release-1",
		}}},
	)
	track := domain.Track{Title: "Song", Artists: []string{"Artist"}, Album: "Original Album", DurationSeconds: 240}
	result, err := registry.SearchTrack(context.Background(), track, Query{}, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 4 {
		t.Fatalf("candidate count = %d, candidates=%#v", len(result.Candidates), result.Candidates)
	}
	smart := result.Candidates[0]
	if smart.Kind != CandidateKindSmart || !smart.Recommended || !smart.AutoAccept || smart.ProviderID != "smart" {
		t.Fatalf("smart candidate = %#v", smart)
	}
	if smart.Evidence.SourceCount != 3 || len(smart.Contributors) != 3 || len(smart.MemberCandidateIDs) != 3 {
		t.Fatalf("smart provenance = %#v", smart)
	}
	if smart.Album.Value != "Original Album" || smart.Lyrics == nil || smart.Lyrics.Source != "Lyrics" || !smart.HasArtwork {
		t.Fatalf("smart fields = %#v", smart)
	}
	if len(smart.Title.Sources) != 3 || smart.ArtworkReferenceID == "" || smart.ArtworkSource == nil || smart.ArtworkSource.ProviderID != "metadata" {
		t.Fatalf("smart field sources = %#v", smart)
	}
	reference, err := registry.ArtworkReference(smart.ArtworkReferenceID)
	if err != nil || reference.ProviderID != "metadata" {
		t.Fatalf("smart artwork reference = %#v err=%v", reference, err)
	}
}

func TestSmartCandidateKeepsReleaseFieldsFromSelectedRelease(t *testing.T) {
	registry := NewRegistry(
		fakeStrategy{descriptor: Descriptor{ID: "original", Name: "Original", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ExternalID: "original-1", Title: "Song", Artists: []string{"Artist"}, Album: "Original Album", AlbumArtists: []string{"Artist"}, Year: 2020, TrackNumber: 3, DurationSeconds: 200,
		}}},
		fakeStrategy{descriptor: Descriptor{ID: "compilation-a", Name: "Compilation A", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ExternalID: "comp-a", Title: "Song", Artists: []string{"Artist"}, Album: "Greatest Hits", AlbumArtists: []string{"Various Artists"}, Year: 2024, TrackNumber: 9, DurationSeconds: 200,
		}}},
		fakeStrategy{descriptor: Descriptor{ID: "compilation-b", Name: "Compilation B", Enabled: true, Health: HealthReady}, candidates: []Candidate{{
			ExternalID: "comp-b", Title: "Song", Artists: []string{"Artist"}, Album: "Greatest Hits", AlbumArtists: []string{"Various Artists"}, Year: 2024, TrackNumber: 9, DurationSeconds: 200,
		}}},
	)
	track := domain.Track{Title: "Song", Artists: []string{"Artist"}, Album: "Original Album", DurationSeconds: 200}
	result, err := registry.SearchTrack(context.Background(), track, Query{}, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	smart := result.Candidates[0]
	if smart.Kind != CandidateKindSmart || smart.Album.Value != "Original Album" || smart.Year.Value != 2020 || smart.TrackNumber.Value != 3 {
		t.Fatalf("smart release fields crossed releases: %#v", smart)
	}
	if smart.Album.Source != "Original" || smart.Year.Source != "Original" {
		t.Fatalf("smart release provenance = %#v", smart)
	}
}

func TestSmartCandidateDoesNotMergeConflictingVersions(t *testing.T) {
	registry := NewRegistry(
		fakeStrategy{descriptor: Descriptor{ID: "live-a", Name: "Live A", Enabled: true, Health: HealthReady}, candidates: []Candidate{{ExternalID: "live-a", Title: "Song (Live)", Artists: []string{"Artist"}, DurationSeconds: 230}}},
		fakeStrategy{descriptor: Descriptor{ID: "live-b", Name: "Live B", Enabled: true, Health: HealthReady}, candidates: []Candidate{{ExternalID: "live-b", Title: "Song 现场版", Artists: []string{"Artist"}, DurationSeconds: 230}}},
		fakeStrategy{descriptor: Descriptor{ID: "remix", Name: "Remix", Enabled: true, Health: HealthReady}, candidates: []Candidate{{ExternalID: "remix", Title: "Song (Remix)", Artists: []string{"Artist"}, DurationSeconds: 230}}},
	)
	track := domain.Track{Title: "Song (Live)", Artists: []string{"Artist"}, DurationSeconds: 230}
	result, err := registry.SearchTrack(context.Background(), track, Query{}, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	smart := result.Candidates[0]
	if smart.Kind != CandidateKindSmart || smart.Evidence.SourceCount != 2 {
		t.Fatalf("version cluster = %#v", smart)
	}
	for _, contributor := range smart.Contributors {
		if contributor.ProviderID == "remix" {
			t.Fatalf("remix was merged into live cluster: %#v", smart)
		}
	}
}

func TestRecordingClusterDoesNotBridgeConflictingVersionsThroughUnqualifiedTitle(t *testing.T) {
	view := func(id, title string) MatchCandidate {
		return MatchCandidate{ID: id, Kind: CandidateKindSource, ProviderID: id, Title: Field[string]{Value: title}, Artists: Field[[]string]{Value: []string{"Artist"}}, DurationSeconds: Field[int64]{Value: 200}}
	}
	clusters := clusterRecordings([]MatchCandidate{view("plain", "Song"), view("live", "Song (Live)"), view("remix", "Song (Remix)")})
	if len(clusters) != 2 {
		t.Fatalf("clusters = %#v", clusters)
	}
}

func TestSmartCandidateRequiresReviewWhenReleaseIsAmbiguous(t *testing.T) {
	registry := NewRegistry(
		fakeStrategy{descriptor: Descriptor{ID: "release-a", Name: "Release A", Enabled: true, Health: HealthReady}, candidates: []Candidate{{ExternalID: "a", Title: "Song", Artists: []string{"Artist"}, Album: "Album A", DurationSeconds: 180}}},
		fakeStrategy{descriptor: Descriptor{ID: "release-b", Name: "Release B", Enabled: true, Health: HealthReady}, candidates: []Candidate{{ExternalID: "b", Title: "Song", Artists: []string{"Artist"}, Album: "Album B", DurationSeconds: 180}}},
	)
	result, err := registry.SearchTrack(context.Background(), domain.Track{Title: "Song", Artists: []string{"Artist"}, DurationSeconds: 180}, Query{}, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	smart := result.Candidates[0]
	if smart.Kind != CandidateKindSmart || smart.AutoAccept || !containsString(smart.Evidence.Conflicts, "存在多个相近发行版本") {
		t.Fatalf("ambiguous release decision = %#v", smart)
	}
	if smart.AlbumArtists.Value == nil || smart.Genres.Value == nil || smart.Composers.Value == nil || smart.Lyricists.Value == nil || smart.MusicBrainzArtistIDs.Value == nil {
		t.Fatalf("smart multi-value fields must serialize as arrays: %#v", smart)
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

func TestRegistrySeparatesArtworkQualityFromIdentityConfidence(t *testing.T) {
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
	if result.Candidates[1].Score != result.Candidates[0].Score || result.Candidates[1].Evidence.IdentityScore != result.Candidates[0].Evidence.IdentityScore {
		t.Fatalf("artwork must not change identity confidence: %#v", result.Candidates)
	}
	if result.Candidates[0].Evidence.AssetQuality <= result.Candidates[1].Evidence.AssetQuality {
		t.Fatalf("artwork quality was not represented separately: %#v", result.Candidates)
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

func TestRegistryDiagnosticsCanIncludeDisabledProviders(t *testing.T) {
	registry := NewRegistry(fakeStrategy{descriptor: Descriptor{ID: "disabled", Name: "Disabled", Enabled: false, Health: HealthDegraded}, candidates: []Candidate{{ExternalID: "1", Title: "Song"}}})
	normal, err := registry.Search(context.Background(), Query{Title: "Song"}, nil, 1)
	if err != nil || len(normal.Providers) != 0 {
		t.Fatalf("normal search = %#v err=%v", normal, err)
	}
	diagnostic, err := registry.SearchWithOptions(context.Background(), Query{Title: "Song"}, nil, 1, SearchOptions{BypassCache: true, IncludeDisabled: true})
	if err != nil || diagnostic.Providers["disabled"].Count != 1 || len(diagnostic.Candidates) != 1 {
		t.Fatalf("diagnostic search = %#v err=%v", diagnostic, err)
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

func TestMigrateProviderConfigurationUpdatesRetiredDefaults(t *testing.T) {
	tests := []struct {
		provider string
		key      string
		old      string
		want     string
	}{
		{"apple", "country", "CN", "HK"},
		{"kugou", "searchEndpoint", "https://mobilecdn.kugou.com/api/v3/search/song", "https://songsearch.kugou.com/song_search_v2"},
		{"kuwo", "lyricsEndpoint", "https://www.kuwo.cn/newh5/singles/songinfoandlrc", "https://www.kuwo.cn/openapi/v1/www/lyric/getlyric"},
	}
	for _, test := range tests {
		values, migrated := migrateProviderConfiguration(test.provider, map[string]string{test.key: test.old, "cookie": "keep-me"})
		if !migrated || values[test.key] != test.want || values["cookie"] != "keep-me" {
			t.Errorf("migrate %s = %#v, %v", test.provider, values, migrated)
		}
	}
}

type configurableAliasStrategy struct {
	*resettableConfigStrategy
	descriptor Descriptor
}

func (strategy *configurableAliasStrategy) Descriptor() Descriptor { return strategy.descriptor }
