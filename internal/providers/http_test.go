package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type providerHTTPSequence struct {
	steps []func(*http.Request) (*http.Response, error)
	calls int
}

func (sequence *providerHTTPSequence) RoundTrip(request *http.Request) (*http.Response, error) {
	sequence.calls++
	index := sequence.calls - 1
	if index >= len(sequence.steps) {
		return nil, fmt.Errorf("unexpected provider request %d", sequence.calls)
	}
	return sequence.steps[index](request)
}

func providerHTTPResponse(request *http.Request, status int, body string, headers http.Header) (*http.Response, error) {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func noProviderRetryWait(context.Context, time.Duration) error { return nil }

func TestGetBytesWithHeadersRetriesTransientResponses(t *testing.T) {
	sequence := &providerHTTPSequence{steps: []func(*http.Request) (*http.Response, error){
		func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("User-Agent") != "test-agent" || request.Header.Get("X-Test") != "yes" {
				t.Fatalf("headers = %#v", request.Header)
			}
			return providerHTTPResponse(request, http.StatusServiceUnavailable, "busy", nil)
		},
		func(request *http.Request) (*http.Response, error) {
			return providerHTTPResponse(request, http.StatusOK, `{"ok":true}`, nil)
		},
	}}
	client := &http.Client{Transport: sequence}
	body, err := getBytesWithHeaders(context.Background(), client, "https://provider.test/search", "test-agent", map[string]string{"X-Test": "yes"}, 1024, providerRetryPolicy{maxAttempts: 3, wait: noProviderRetryWait, now: func() time.Time { return time.Unix(100, 0) }})
	if err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("body=%s err=%v", body, err)
	}
	if sequence.calls != 2 {
		t.Fatalf("calls=%d, want 2", sequence.calls)
	}
}

func TestGetBytesWithHeadersHonorsRetryAfterAndCapsDelay(t *testing.T) {
	now := time.Unix(100, 0)
	sequence := &providerHTTPSequence{steps: []func(*http.Request) (*http.Response, error){
		func(request *http.Request) (*http.Response, error) {
			return providerHTTPResponse(request, http.StatusTooManyRequests, "slow down", http.Header{"Retry-After": {"9"}})
		},
		func(request *http.Request) (*http.Response, error) {
			return providerHTTPResponse(request, http.StatusOK, "ok", nil)
		},
	}}
	var waits []time.Duration
	body, err := getBytesWithHeaders(context.Background(), &http.Client{Transport: sequence}, "https://provider.test/search", "", nil, 1024, providerRetryPolicy{
		maxAttempts: 2,
		wait:        func(_ context.Context, delay time.Duration) error { waits = append(waits, delay); return nil },
		now:         func() time.Time { return now },
	})
	if err != nil || string(body) != "ok" {
		t.Fatalf("body=%s err=%v", body, err)
	}
	if len(waits) != 1 || waits[0] != providerRetryMaxDelay {
		t.Fatalf("waits=%v, want one capped delay of %v", waits, providerRetryMaxDelay)
	}
}

func TestGetBytesWithHeadersDoesNotRetryPermanentHTTPFailure(t *testing.T) {
	sequence := &providerHTTPSequence{steps: []func(*http.Request) (*http.Response, error){
		func(request *http.Request) (*http.Response, error) {
			return providerHTTPResponse(request, http.StatusUnauthorized, strings.Repeat("x", 600), nil)
		},
	}}
	_, err := getBytesWithHeaders(context.Background(), &http.Client{Transport: sequence}, "https://provider.test/search", "", nil, 1024, providerRetryPolicy{maxAttempts: 3, wait: noProviderRetryWait, now: time.Now})
	var httpError *HTTPError
	if !errors.As(err, &httpError) || httpError.Status != http.StatusUnauthorized {
		t.Fatalf("error=%v, want HTTP 401", err)
	}
	if len([]rune(httpError.Message)) != maxProviderErrorMessage+1 || sequence.calls != 1 {
		t.Fatalf("message length=%d calls=%d", len([]rune(httpError.Message)), sequence.calls)
	}
}

func TestErrorHintClassifiesAuthRateAndTransportFailures(t *testing.T) {
	if hint := ErrorHint(&HTTPError{Status: http.StatusUnauthorized}); !strings.Contains(hint, "鉴权") {
		t.Fatalf("auth hint = %q", hint)
	}
	if hint := ErrorHint(&BusinessError{Provider: "kuwo", Code: "status=401", Message: "expired"}); !strings.Contains(hint, "Cookie") {
		t.Fatalf("business auth hint = %q", hint)
	}
	if hint := ErrorHint(&HTTPError{Status: http.StatusTooManyRequests}); !strings.Contains(hint, "限流") {
		t.Fatalf("rate hint = %q", hint)
	}
	if hint := ErrorHint(&TransportError{Err: errors.New("offline")}); !strings.Contains(hint, "网络") {
		t.Fatalf("transport hint = %q", hint)
	}
}

func TestGetBytesWithHeadersRetriesTransportFailure(t *testing.T) {
	sequence := &providerHTTPSequence{steps: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) { return nil, errors.New("connection reset") },
		func(*http.Request) (*http.Response, error) { return nil, errors.New("connection reset") },
		func(*http.Request) (*http.Response, error) { return nil, errors.New("connection reset") },
	}}
	_, err := getBytesWithHeaders(context.Background(), &http.Client{Transport: sequence}, "https://provider.test/search", "", nil, 1024, providerRetryPolicy{maxAttempts: 3, wait: noProviderRetryWait, now: time.Now})
	var transportError *TransportError
	if !errors.As(err, &transportError) || sequence.calls != 3 {
		t.Fatalf("error=%v calls=%d", err, sequence.calls)
	}
}

func TestRetryAfterParsesHTTPDate(t *testing.T) {
	now := time.Unix(100, 0)
	header := now.Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if delay := retryAfterAt(header, now); delay <= 0 {
		t.Fatalf("delay=%v for header %q", delay, header)
	}
	if retryAfterAt("invalid", now) != 0 {
		t.Fatal("invalid Retry-After should be ignored")
	}
}
