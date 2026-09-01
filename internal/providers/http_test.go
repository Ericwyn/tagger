package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestValidateProxyURL(t *testing.T) {
	for _, value := range []string{
		"socks5://127.0.0.1:1080",
		"http://user:password@127.0.0.1:7890",
		"http://127.0.0.1:7890/proxy",
		"http://127.0.0.1:7890?mode=tunnel",
		"http://127.0.0.1:7890#proxy",
		"127.0.0.1:7890",
	} {
		if _, err := ValidateProxyURL(value); err == nil {
			t.Errorf("proxy URL %q unexpectedly accepted", value)
		}
	}
	for input, expected := range map[string]string{
		"":                                "",
		" http://127.0.0.1:7890/ ":        "http://127.0.0.1:7890",
		"https://proxy.example.test:8443": "https://proxy.example.test:8443",
	} {
		actual, err := ValidateProxyURL(input)
		if err != nil || actual != expected {
			t.Errorf("ValidateProxyURL(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
}

func TestHTTPClientWithProxyRoutesRequestThroughExplicitProxy(t *testing.T) {
	requests := make(chan *http.Request, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Clone(request.Context())
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("proxied"))
	}))
	defer proxy.Close()

	client, err := HTTPClientWithProxy(&http.Client{Timeout: time.Second}, NewGate(0), proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get("http://provider.invalid/search?q=song")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "proxied" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	select {
	case request := <-requests:
		if request.URL.Host != "provider.invalid" || request.URL.Path != "/search" || request.URL.Query().Get("q") != "song" {
			t.Fatalf("proxy request URL = %s", request.URL)
		}
	case <-time.After(time.Second):
		t.Fatal("explicit proxy did not receive provider request")
	}
}

func TestHTTPClientWithEmptyProxyPreservesBaseProxySelection(t *testing.T) {
	marker, _ := url.Parse("http://environment-proxy.test:8080")
	base := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(marker)}}
	client, err := HTTPClientWithProxy(base, NewGate(0), "")
	if err != nil {
		t.Fatal(err)
	}
	gated, ok := client.Transport.(*gatedRoundTripper)
	if !ok {
		t.Fatalf("transport = %T", client.Transport)
	}
	transport, ok := gated.base.(*http.Transport)
	if !ok {
		t.Fatalf("base transport = %T", gated.base)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://provider.test/search", nil)
	selected, err := transport.Proxy(request)
	if err != nil || selected.String() != marker.String() {
		t.Fatalf("selected proxy = %v, %v; want %s", selected, err, marker)
	}
}

type gateTrackingTransport struct {
	entered chan struct{}
}

func (transport *gateTrackingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.entered <- struct{}{}
	return providerHTTPResponse(request, http.StatusOK, "ok", nil)
}

func TestProviderGateHoldsSingleRequestSlotUntilBodyCloses(t *testing.T) {
	base := &gateTrackingTransport{entered: make(chan struct{}, 2)}
	client := WrapHTTPClient(&http.Client{Transport: base}, NewGate(0))

	firstResult := make(chan *http.Response, 1)
	firstError := make(chan error, 1)
	go func() {
		response, err := client.Get("https://provider.test/first")
		firstResult <- response
		firstError <- err
	}()
	<-base.entered
	first := <-firstResult
	if err := <-firstError; err != nil {
		t.Fatal(err)
	}

	secondResult := make(chan *http.Response, 1)
	secondError := make(chan error, 1)
	go func() {
		response, err := client.Get("https://provider.test/second")
		secondResult <- response
		secondError <- err
	}()
	select {
	case <-base.entered:
		t.Fatal("second request entered transport before first body closed")
	case <-time.After(30 * time.Millisecond):
	}
	if err := first.Body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-base.entered:
	case <-time.After(time.Second):
		t.Fatal("second request did not start after first body closed")
	}
	second := <-secondResult
	if err := <-secondError; err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
}

func TestProviderGateIsSharedAcrossClientsAndIndependentAcrossSources(t *testing.T) {
	sharedGate := NewGate(0)
	apiTransport := &gateTrackingTransport{entered: make(chan struct{}, 1)}
	artworkTransport := &gateTrackingTransport{entered: make(chan struct{}, 1)}
	otherTransport := &gateTrackingTransport{entered: make(chan struct{}, 1)}
	apiClient := WrapHTTPClient(&http.Client{Transport: apiTransport}, sharedGate)
	artworkClient := WrapHTTPClient(&http.Client{Transport: artworkTransport}, sharedGate)
	otherClient := WrapHTTPClient(&http.Client{Transport: otherTransport}, NewGate(0))

	apiResponse, err := apiClient.Get("https://source-a.test/api")
	if err != nil {
		t.Fatal(err)
	}
	<-apiTransport.entered
	artworkResult := make(chan *http.Response, 1)
	artworkError := make(chan error, 1)
	go func() {
		response, err := artworkClient.Get("https://source-a.test/artwork")
		artworkResult <- response
		artworkError <- err
	}()
	select {
	case <-artworkTransport.entered:
		t.Fatal("artwork request bypassed the source's API request slot")
	case <-time.After(30 * time.Millisecond):
	}

	otherResponse, err := otherClient.Get("https://source-b.test/api")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-otherTransport.entered:
	case <-time.After(time.Second):
		t.Fatal("a different provider gate was blocked by source A")
	}
	otherResponse.Body.Close()
	apiResponse.Body.Close()
	select {
	case <-artworkTransport.entered:
	case <-time.After(time.Second):
		t.Fatal("artwork request did not resume after API body closed")
	}
	artworkResponse := <-artworkResult
	if err := <-artworkError; err != nil {
		t.Fatal(err)
	}
	artworkResponse.Body.Close()
}

func TestProviderGateCancelsWhileWaitingForRequestSlot(t *testing.T) {
	base := &gateTrackingTransport{entered: make(chan struct{}, 2)}
	client := WrapHTTPClient(&http.Client{Transport: base}, NewGate(0))
	first, err := client.Get("https://provider.test/first")
	if err != nil {
		t.Fatal(err)
	}
	<-base.entered

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://provider.test/second", nil)
	_, err = client.Do(request)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting request error = %v", err)
	}
	select {
	case <-base.entered:
		t.Fatal("cancelled request reached base transport")
	default:
	}
	first.Body.Close()
}

func TestProviderGateAppliesIntervalToAutomaticRetries(t *testing.T) {
	starts := make([]time.Time, 0, 2)
	sequence := &providerHTTPSequence{steps: []func(*http.Request) (*http.Response, error){
		func(request *http.Request) (*http.Response, error) {
			starts = append(starts, time.Now())
			return providerHTTPResponse(request, http.StatusServiceUnavailable, "busy", nil)
		},
		func(request *http.Request) (*http.Response, error) {
			starts = append(starts, time.Now())
			return providerHTTPResponse(request, http.StatusOK, "ok", nil)
		},
	}}
	client := WrapHTTPClient(&http.Client{Transport: sequence}, NewGate(40*time.Millisecond))
	body, err := getBytesWithHeaders(context.Background(), client, "https://provider.test/search", "", nil, 1024, providerRetryPolicy{
		maxAttempts: 2, wait: noProviderRetryWait, now: time.Now,
	})
	if err != nil || string(body) != "ok" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if len(starts) != 2 || starts[1].Sub(starts[0]) < 30*time.Millisecond {
		t.Fatalf("retry starts = %v", starts)
	}
}

func TestProviderGateRuntimeIntervalUpdateAppliesToNextRequest(t *testing.T) {
	gate := NewGate(time.Second)
	base := &gateTrackingTransport{entered: make(chan struct{}, 2)}
	client := WrapHTTPClient(&http.Client{Transport: base}, gate)
	response, err := client.Get("https://provider.test/first")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	response.Body.Close()
	<-base.entered

	gate.SetInterval(0)
	started := time.Now()
	response, err = client.Get("https://provider.test/second")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("updated zero interval still waited %v", elapsed)
	}
}
