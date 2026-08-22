package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxProviderResponseBytes = 2 << 20

type HTTPError struct {
	Status     int
	RetryAfter time.Duration
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("provider HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("provider HTTP %d", e.Status)
}

func GetJSON(ctx context.Context, client *http.Client, endpoint, userAgent string, target any) error {
	return GetJSONWithHeaders(ctx, client, endpoint, userAgent, nil, target)
}

// GetJSONWithHeaders is the common bounded JSON transport used by providers.
// A few public music endpoints require an Origin/Referer pair in addition to
// User-Agent, so keeping the header extension here avoids duplicating response
// size and status handling in each strategy.
func GetJSONWithHeaders(ctx context.Context, client *http.Client, endpoint, userAgent string, headers map[string]string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	for name, value := range headers {
		if strings.TrimSpace(name) != "" && value != "" {
			request.Header.Set(name, value)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxProviderResponseBytes {
		return fmt.Errorf("provider response exceeds %d bytes", maxProviderResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &HTTPError{Status: response.StatusCode, RetryAfter: retryAfter(response.Header.Get("Retry-After")), Message: strings.TrimSpace(string(body))}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode provider response: %w", err)
	}
	return nil
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

type Gate struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func NewGate(interval time.Duration) *Gate { return &Gate{interval: interval} }

func (g *Gate) Wait(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	interval := g.interval
	if interval <= 0 {
		g.mu.Unlock()
		return nil
	}
	now := time.Now()
	reserved := g.next
	if reserved.Before(now) {
		reserved = now
	}
	g.next = reserved.Add(interval)
	g.mu.Unlock()

	if delay := time.Until(reserved); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (g *Gate) SetInterval(interval time.Duration) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.interval = interval
	g.mu.Unlock()
}

func (g *Gate) Interval() time.Duration {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.interval
}
