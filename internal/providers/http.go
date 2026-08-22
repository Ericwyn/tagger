package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxProviderResponseBytes = 2 << 20

const (
	defaultProviderMaxAttempts = 3
	providerRetryBaseDelay     = 100 * time.Millisecond
	providerRetryMaxDelay      = 2 * time.Second
	maxProviderErrorMessage    = 512
)

type HTTPError struct {
	Status     int
	RetryAfter time.Duration
	Message    string
}

// BusinessError represents a provider returning HTTP 200 but rejecting the
// request in its JSON envelope. Public music APIs commonly use this shape for
// expired cookies, throttling and retired endpoints, so surfacing it as a
// typed error keeps the diagnostics panel actionable instead of showing an
// empty result set.
type BusinessError struct {
	Provider  string
	Code      string
	Message   string
	Retryable bool
}

// ErrorHint turns common authorization/rate-limit/network failures into a
// concise action for the single-user diagnostics UI.
func ErrorHint(err error) string {
	if err == nil {
		return ""
	}
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		switch httpError.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "请检查数据源的鉴权头或 Cookie，确认接口地址仍然有效"
		case http.StatusTooManyRequests:
			return "数据源触发限流，请降低请求频率或稍后重试"
		case http.StatusRequestTimeout:
			return "数据源响应超时，请稍后重试或检查接口地址"
		}
		if httpError.Status >= 500 {
			return "数据源服务暂时不可用，请稍后重试或切换备用接口"
		}
	}
	var businessError *BusinessError
	if errors.As(err, &businessError) {
		code := strings.ToLower(strings.TrimSpace(businessError.Code))
		if strings.Contains(code, "401") || strings.Contains(code, "unauthorized") || strings.Contains(code, "login") {
			return "请更新数据源 Cookie/鉴权头，网页登录凭证可能已过期"
		}
		if strings.Contains(code, "403") || strings.Contains(code, "forbidden") {
			return "当前接口拒绝访问，请检查 Cookie、接口地址或来源站点限制"
		}
		if businessError.Retryable || strings.Contains(code, "429") || strings.Contains(code, "rate") {
			return "数据源触发限流，请降低请求频率或稍后重试"
		}
		return "数据源返回了业务错误，请检查接口版本和配置"
	}
	var transportError *TransportError
	if errors.As(err, &transportError) {
		return "无法连接数据源，请检查网络、代理或 API Base URL"
	}
	return "请检查数据源配置并查看详细日志"
}

func (e *BusinessError) Error() string {
	if e == nil {
		return "provider business error"
	}
	label := strings.TrimSpace(e.Provider)
	if label == "" {
		label = "provider"
	}
	code := strings.TrimSpace(e.Code)
	message := strings.TrimSpace(e.Message)
	if code != "" && message != "" {
		return fmt.Sprintf("%s rejected request (code %s): %s", label, code, message)
	}
	if message != "" {
		return fmt.Sprintf("%s rejected request: %s", label, message)
	}
	if code != "" {
		return fmt.Sprintf("%s rejected request (code %s)", label, code)
	}
	return label + " rejected request"
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("provider HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("provider HTTP %d", e.Status)
}

// TransportError marks a network or response-body failure that is safe to
// retry for an idempotent provider GET. The original error remains available
// through errors.Is/As for diagnostics.
type TransportError struct{ Err error }

func (e *TransportError) Error() string {
	if e == nil || e.Err == nil {
		return "provider transport failed"
	}
	return "provider transport failed: " + e.Err.Error()
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func GetJSON(ctx context.Context, client *http.Client, endpoint, userAgent string, target any) error {
	return GetJSONWithHeaders(ctx, client, endpoint, userAgent, nil, target)
}

// GetJSONWithHeaders is the common bounded JSON transport used by providers.
// A few public music endpoints require an Origin/Referer pair in addition to
// User-Agent, so keeping the header extension here avoids duplicating response
// size and status handling in each strategy.
func GetJSONWithHeaders(ctx context.Context, client *http.Client, endpoint, userAgent string, headers map[string]string, target any) error {
	body, err := GetBytesWithHeaders(ctx, client, endpoint, userAgent, headers, maxProviderResponseBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode provider response: %w", err)
	}
	return nil
}

// GetBytesWithHeaders performs a bounded idempotent GET with a short,
// context-aware retry policy. It is also used for text lyrics endpoints whose
// payload is not JSON. Callers should not use it for non-idempotent requests.
func GetBytesWithHeaders(ctx context.Context, client *http.Client, endpoint, userAgent string, headers map[string]string, maxBytes int) ([]byte, error) {
	return getBytesWithHeaders(ctx, client, endpoint, userAgent, headers, maxBytes, providerRetryPolicy{
		maxAttempts: defaultProviderMaxAttempts,
		wait:        sleepProviderRetry,
		now:         time.Now,
	})
}

type providerRetryPolicy struct {
	maxAttempts int
	wait        func(context.Context, time.Duration) error
	now         func() time.Time
}

func getBytesWithHeaders(ctx context.Context, client *http.Client, endpoint, userAgent string, headers map[string]string, maxBytes int, policy providerRetryPolicy) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = maxProviderResponseBytes
	}
	if policy.maxAttempts <= 0 {
		policy.maxAttempts = 1
	}
	if policy.wait == nil {
		policy.wait = sleepProviderRetry
	}
	if policy.now == nil {
		policy.now = time.Now
	}
	if client == nil {
		client = http.DefaultClient
	}
	for attempt := 1; attempt <= policy.maxAttempts; attempt++ {
		body, err := getBytesOnce(ctx, client, endpoint, userAgent, headers, maxBytes, policy.now())
		if err == nil {
			return body, nil
		}
		if !retryableProviderError(err) || attempt == policy.maxAttempts {
			return nil, err
		}
		if err := policy.wait(ctx, providerRetryDelay(attempt, retryAfterFromError(err))); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("provider retry attempts exhausted")
}

func getBytesOnce(ctx context.Context, client *http.Client, endpoint, userAgent string, headers map[string]string, maxBytes int, now time.Time) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
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
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, &TransportError{Err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("provider response exceeds %d bytes", maxBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPError{
			Status: response.StatusCode, RetryAfter: retryAfterAt(response.Header.Get("Retry-After"), now),
			Message: truncateProviderMessage(strings.TrimSpace(string(body))),
		}
	}
	return body, nil
}

func retryableProviderError(err error) bool {
	var businessError *BusinessError
	if errors.As(err, &businessError) {
		return businessError.Retryable
	}
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		return httpError.Status == http.StatusRequestTimeout || httpError.Status == http.StatusTooManyRequests || httpError.Status >= 500
	}
	var transportError *TransportError
	return errors.As(err, &transportError)
}

func retryAfterFromError(err error) time.Duration {
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		return httpError.RetryAfter
	}
	return 0
}

func providerRetryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > providerRetryMaxDelay {
			return providerRetryMaxDelay
		}
		return retryAfter
	}
	delay := providerRetryBaseDelay
	for index := 1; index < attempt; index++ {
		delay *= 2
		if delay >= providerRetryMaxDelay {
			return providerRetryMaxDelay
		}
	}
	return delay
}

func sleepProviderRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return retryAfterAt(value, time.Now())
}

func retryAfterAt(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	date, err := http.ParseTime(value)
	if err != nil || !date.After(now) {
		return 0
	}
	return date.Sub(now)
}

func truncateProviderMessage(value string) string {
	if len([]rune(value)) <= maxProviderErrorMessage {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxProviderErrorMessage]) + "…"
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
