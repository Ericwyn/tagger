package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrProviderNotFound = errors.New("provider not found")
var ErrArtworkReferenceNotFound = errors.New("artwork reference not found")
var ErrProviderUnavailable = errors.New("provider is unavailable")
var ErrProviderNotConfigurable = errors.New("provider does not expose runtime configuration")
var ErrProviderConfigInvalid = errors.New("provider configuration is invalid")

type Persistence interface {
	LoadProviderCache(context.Context, string) ([]byte, bool, error)
	SaveProviderCache(context.Context, string, string, []byte, time.Duration) error
	DeleteExpiredProviderCache(context.Context) error
	LoadArtworkReferences(context.Context) ([]byte, error)
	SaveArtworkReference(context.Context, string, string, string, time.Time) error
	DeleteExpiredArtworkReferences(context.Context) error
	LoadProviderSettings(context.Context) (map[string]bool, error)
	SaveProviderEnabled(context.Context, string, bool) error
	LoadProviderConfigurations(context.Context) (map[string]map[string]string, error)
	SaveProviderConfiguration(context.Context, string, map[string]string) error
}

type ArtworkReference struct {
	CandidateID string    `json:"candidateId"`
	ProviderID  string    `json:"providerId"`
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type Registry struct {
	strategies  map[string]Strategy
	order       []string
	artworkMu   sync.RWMutex
	artworks    map[string]ArtworkReference
	persistence Persistence
	settingsMu  sync.RWMutex
	enabled     map[string]bool
	configs     map[string]map[string]string
	configError map[string]string
}

func NewRegistry(strategies ...Strategy) *Registry {
	registry := &Registry{
		strategies: make(map[string]Strategy), order: make([]string, 0, len(strategies)), artworks: make(map[string]ArtworkReference), enabled: make(map[string]bool), configs: make(map[string]map[string]string), configError: make(map[string]string),
	}
	for _, strategy := range strategies {
		if strategy == nil {
			continue
		}
		id := strategy.Descriptor().ID
		if id == "" {
			continue
		}
		if _, exists := registry.strategies[id]; !exists {
			registry.order = append(registry.order, id)
		}
		registry.strategies[id] = strategy
	}
	return registry
}

func (r *Registry) SetPersistence(ctx context.Context, persistence Persistence) error {
	r.persistence = persistence
	if persistence == nil {
		return nil
	}
	settings, err := persistence.LoadProviderSettings(ctx)
	if err != nil {
		return err
	}
	if err := persistence.DeleteExpiredArtworkReferences(ctx); err != nil {
		return err
	}
	configurations, err := persistence.LoadProviderConfigurations(ctx)
	if err != nil {
		return err
	}
	payload, err := persistence.LoadArtworkReferences(ctx)
	if err != nil {
		return err
	}
	var references []ArtworkReference
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &references); err != nil {
			return err
		}
	}
	r.artworkMu.Lock()
	for _, reference := range references {
		r.artworks[reference.CandidateID] = reference
	}
	r.artworkMu.Unlock()
	r.settingsMu.Lock()
	r.enabled = settings
	r.configs = cloneProviderConfigurations(configurations)
	r.settingsMu.Unlock()
	for id, values := range configurations {
		strategy, found := r.strategies[id]
		if !found {
			continue
		}
		configurable, ok := strategy.(Configurable)
		if !ok || len(values) == 0 {
			continue
		}
		if err := configurable.Configure(values); err != nil {
			r.settingsMu.Lock()
			r.configError[id] = err.Error()
			r.settingsMu.Unlock()
		}
	}
	return persistence.DeleteExpiredProviderCache(ctx)
}

func (r *Registry) SetEnabled(ctx context.Context, id string, enabled bool) (Descriptor, error) {
	strategy, found := r.strategies[id]
	if !found {
		return Descriptor{}, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
	}
	base := strategy.Descriptor()
	if enabled && base.Experimental && base.Health == HealthDisabled {
		return Descriptor{}, fmt.Errorf("%w: %s adapter is not implemented", ErrProviderUnavailable, id)
	}
	if r.persistence != nil {
		if err := r.persistence.SaveProviderEnabled(ctx, id, enabled); err != nil {
			return Descriptor{}, err
		}
	}
	r.settingsMu.Lock()
	r.enabled[id] = enabled
	r.settingsMu.Unlock()
	return r.descriptor(id), nil
}

func (r *Registry) descriptor(id string) Descriptor {
	descriptor := r.strategies[id].Descriptor()
	if configurable, ok := r.strategies[id].(Configurable); ok {
		descriptor.Config = sanitizeConfigFields(configurable.ConfigFields())
	}
	r.settingsMu.RLock()
	enabled, overridden := r.enabled[id]
	configError := r.configError[id]
	r.settingsMu.RUnlock()
	if overridden {
		descriptor.Enabled = enabled
	}
	if configError != "" {
		descriptor.ConfigError = configError
		if descriptor.Enabled {
			descriptor.Health = HealthMisconfigured
		}
	}
	if !descriptor.Enabled {
		descriptor.Health = HealthDisabled
	}
	descriptor.Capabilities = append([]string(nil), descriptor.Capabilities...)
	return descriptor
}

// SetConfig applies only the supplied keys. Omitting a key keeps its current
// value, which lets the UI leave masked secrets untouched.
func (r *Registry) SetConfig(ctx context.Context, id string, values map[string]string) (Descriptor, error) {
	strategy, found := r.strategies[id]
	if !found {
		return Descriptor{}, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
	}
	configurable, ok := strategy.(Configurable)
	if !ok {
		return Descriptor{}, fmt.Errorf("%w: %s", ErrProviderNotConfigurable, id)
	}
	schema := configurable.ConfigFields()
	allowed := make(map[string]struct{}, len(schema))
	for _, field := range schema {
		allowed[field.Key] = struct{}{}
	}
	for key := range values {
		if _, exists := allowed[key]; !exists {
			return Descriptor{}, fmt.Errorf("%w: unknown field %q", ErrProviderConfigInvalid, key)
		}
	}
	if err := configurable.Configure(values); err != nil {
		return Descriptor{}, fmt.Errorf("%w: %v", ErrProviderConfigInvalid, err)
	}
	r.settingsMu.Lock()
	if r.configs[id] == nil {
		r.configs[id] = make(map[string]string)
	}
	for key, value := range values {
		r.configs[id][key] = value
	}
	delete(r.configError, id)
	configuration := cloneStringMap(r.configs[id])
	r.settingsMu.Unlock()
	if r.persistence != nil {
		if err := r.persistence.SaveProviderConfiguration(ctx, id, configuration); err != nil {
			return Descriptor{}, err
		}
	}
	return r.descriptor(id), nil
}

func sanitizeConfigFields(fields []ConfigField) []ConfigField {
	result := make([]ConfigField, len(fields))
	for index, field := range fields {
		if field.Secret {
			field.Configured = field.Configured || field.Value != ""
			field.Value = ""
		}
		result[index] = field
	}
	return result
}

func cloneProviderConfigurations(values map[string]map[string]string) map[string]map[string]string {
	result := make(map[string]map[string]string, len(values))
	for id, config := range values {
		result[id] = cloneStringMap(config)
	}
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func (r *Registry) ArtworkReference(candidateID string) (ArtworkReference, error) {
	r.artworkMu.RLock()
	reference, found := r.artworks[candidateID]
	r.artworkMu.RUnlock()
	if !found || time.Now().After(reference.ExpiresAt) {
		if found {
			r.artworkMu.Lock()
			delete(r.artworks, candidateID)
			r.artworkMu.Unlock()
		}
		return ArtworkReference{}, ErrArtworkReferenceNotFound
	}
	return reference, nil
}

func (r *Registry) rememberArtwork(ctx context.Context, candidateID, providerID, artworkURL string) {
	if artworkURL == "" {
		return
	}
	reference := ArtworkReference{
		CandidateID: candidateID, ProviderID: providerID, URL: artworkURL, ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	r.artworkMu.Lock()
	r.artworks[candidateID] = reference
	r.artworkMu.Unlock()
	if r.persistence != nil {
		_ = r.persistence.SaveArtworkReference(ctx, reference.CandidateID, reference.ProviderID, reference.URL, reference.ExpiresAt)
	}
}

func (r *Registry) Descriptors() []Descriptor {
	result := make([]Descriptor, 0, len(r.order))
	for _, id := range r.order {
		descriptor := r.descriptor(id)
		result = append(result, descriptor)
	}
	return result
}

func (r *Registry) Descriptor(id string) (Descriptor, bool) {
	strategy, ok := r.strategies[id]
	if !ok {
		return Descriptor{}, false
	}
	_ = strategy
	return r.descriptor(id), true
}

func (r *Registry) Search(ctx context.Context, query Query, providerIDs []string, limit int) (SearchResult, error) {
	if query.Title == "" {
		return SearchResult{}, fmt.Errorf("title is required")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	selected := make([]Strategy, 0)
	if len(providerIDs) == 0 {
		for _, id := range r.order {
			strategy := r.strategies[id]
			if descriptor := r.descriptor(id); descriptor.Enabled && descriptor.Health != HealthDisabled {
				selected = append(selected, strategy)
			}
		}
	} else {
		for _, id := range providerIDs {
			strategy, ok := r.strategies[id]
			if !ok {
				return SearchResult{}, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
			}
			if r.descriptor(id).Enabled {
				selected = append(selected, strategy)
			}
		}
	}

	outcomes := make(chan searchOutcome, len(selected))
	var wait sync.WaitGroup
	for _, strategy := range selected {
		wait.Add(1)
		go func(strategy Strategy) {
			defer wait.Done()
			started := time.Now()
			descriptor := r.descriptor(strategy.Descriptor().ID)
			cacheKey := providerCacheKey(descriptor.ID, query, limit)
			if r.persistence != nil {
				if payload, found, cacheErr := r.persistence.LoadProviderCache(ctx, cacheKey); cacheErr == nil && found {
					var candidates []Candidate
					if json.Unmarshal(payload, &candidates) == nil {
						outcomes <- searchOutcome{descriptor: descriptor, candidates: candidates, duration: time.Since(started), cached: true}
						return
					}
				}
			}
			candidates, err := strategy.Search(ctx, query, limit)
			if err == nil && r.persistence != nil {
				if payload, marshalErr := json.Marshal(candidates); marshalErr == nil {
					_ = r.persistence.SaveProviderCache(ctx, cacheKey, descriptor.ID, payload, providerCacheTTL(descriptor.ID))
				}
			}
			outcomes <- searchOutcome{descriptor: descriptor, candidates: candidates, err: err, duration: time.Since(started)}
		}(strategy)
	}
	wait.Wait()
	close(outcomes)

	result := SearchResult{Candidates: []MatchCandidate{}, Providers: make(map[string]ProviderResult, len(selected))}
	for outcome := range outcomes {
		providerResult := ProviderResult{Status: "ok", Count: len(outcome.candidates), LatencyMS: outcome.duration.Milliseconds(), Cached: outcome.cached}
		if outcome.err != nil {
			providerResult.Status = "error"
			providerResult.Error = outcome.err.Error()
			providerResult.Retryable = isRetryable(outcome.err)
		} else {
			for _, candidate := range outcome.candidates {
				view := toView(query, outcome.descriptor, candidate)
				result.Candidates = append(result.Candidates, view)
				r.rememberArtwork(ctx, view.ID, outcome.descriptor.ID, candidate.ArtworkURL)
			}
		}
		result.Providers[outcome.descriptor.ID] = providerResult
	}
	sortViews(result.Candidates)
	return result, nil
}

func providerCacheKey(providerID string, query Query, limit int) string {
	artists := make([]string, len(query.Artists))
	for index, artist := range query.Artists {
		artists[index] = normalize(artist)
	}
	payload, _ := json.Marshal(struct {
		Provider, Title, Album string
		Artists                []string
		Duration               int64
		Limit                  int
	}{providerID, normalize(query.Title), normalize(query.Album), artists, query.DurationSeconds, limit})
	digest := sha256.Sum256(payload)
	// Bump the cache namespace when candidate enrichment changes. This avoids
	// serving metadata-only results cached by the old NetEase adapter after
	// lyrics-capable providers are upgraded.
	return "provider-search-v2-" + hex.EncodeToString(digest[:])
}

func providerCacheTTL(providerID string) time.Duration {
	if providerID == "musicbrainz" {
		return 7 * 24 * time.Hour
	}
	return 24 * time.Hour
}

func isRetryable(err error) bool {
	var httpError *HTTPError
	return errors.As(err, &httpError) && (httpError.Status == 429 || httpError.Status >= 500)
}

type Placeholder struct{ descriptor Descriptor }

func NewPlaceholder(descriptor Descriptor) *Placeholder { return &Placeholder{descriptor: descriptor} }

func (p *Placeholder) Descriptor() Descriptor { return p.descriptor }

func (p *Placeholder) Search(context.Context, Query, int) ([]Candidate, error) {
	return nil, fmt.Errorf("provider %s is not configured", p.descriptor.ID)
}
