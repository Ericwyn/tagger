package providers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrProviderNotFound = errors.New("provider not found")

type Registry struct {
	strategies map[string]Strategy
	order      []string
}

func NewRegistry(strategies ...Strategy) *Registry {
	registry := &Registry{strategies: make(map[string]Strategy), order: make([]string, 0, len(strategies))}
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

func (r *Registry) Descriptors() []Descriptor {
	result := make([]Descriptor, 0, len(r.order))
	for _, id := range r.order {
		descriptor := r.strategies[id].Descriptor()
		descriptor.Capabilities = append([]string(nil), descriptor.Capabilities...)
		result = append(result, descriptor)
	}
	return result
}

func (r *Registry) Descriptor(id string) (Descriptor, bool) {
	strategy, ok := r.strategies[id]
	if !ok {
		return Descriptor{}, false
	}
	descriptor := strategy.Descriptor()
	descriptor.Capabilities = append([]string(nil), descriptor.Capabilities...)
	return descriptor, true
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
			if descriptor := strategy.Descriptor(); descriptor.Enabled && descriptor.Health != HealthDisabled {
				selected = append(selected, strategy)
			}
		}
	} else {
		for _, id := range providerIDs {
			strategy, ok := r.strategies[id]
			if !ok {
				return SearchResult{}, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
			}
			if strategy.Descriptor().Enabled {
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
			candidates, err := strategy.Search(ctx, query, limit)
			outcomes <- searchOutcome{descriptor: strategy.Descriptor(), candidates: candidates, err: err, duration: time.Since(started)}
		}(strategy)
	}
	wait.Wait()
	close(outcomes)

	result := SearchResult{Candidates: []MatchCandidate{}, Providers: make(map[string]ProviderResult, len(selected))}
	for outcome := range outcomes {
		providerResult := ProviderResult{Status: "ok", Count: len(outcome.candidates), LatencyMS: outcome.duration.Milliseconds()}
		if outcome.err != nil {
			providerResult.Status = "error"
			providerResult.Error = outcome.err.Error()
			providerResult.Retryable = isRetryable(outcome.err)
		} else {
			for _, candidate := range outcome.candidates {
				result.Candidates = append(result.Candidates, toView(query, outcome.descriptor, candidate))
			}
		}
		result.Providers[outcome.descriptor.ID] = providerResult
	}
	sortViews(result.Candidates)
	return result, nil
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
