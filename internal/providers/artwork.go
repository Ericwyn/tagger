package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericwyn/tagger/internal/artwork"
)

var ErrUnsafeArtworkURL = errors.New("unsafe provider artwork URL")

func DownloadArtwork(ctx context.Context, reference ArtworkReference, client *http.Client) (artwork.Asset, error) {
	parsed, err := validateArtworkURL(reference.ProviderID, reference.URL)
	if err != nil {
		return artwork.Asset{}, err
	}
	if client == nil {
		client = safeArtworkClient(reference.ProviderID)
	}
	body, err := GetBytesWithHeaders(ctx, client, parsed.String(), "Tagger/0.1 (+https://github.com/ericwyn/tagger)", map[string]string{
		"Accept": "image/jpeg, image/png, image/webp",
	}, artwork.MaxBytes)
	if err != nil {
		return artwork.Asset{}, fmt.Errorf("download provider artwork: %w", err)
	}
	// Provider CDNs often serve an image with a stale or generic Content-Type
	// (NetEase has returned image/jpeg for PNG bytes). The bytes are the source
	// of truth here; Validate still sniffs, decodes, size-checks and dimensions-
	// checks the actual payload. Local uploads continue to use strict declared
	// MIME validation in the server path.
	asset, err := artwork.Validate(body, "")
	if err != nil {
		return artwork.Asset{}, err
	}
	return asset, nil
}

func validateArtworkURL(providerID, value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("%w: HTTPS provider URL required", ErrUnsafeArtworkURL)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	allowed := false
	switch providerID {
	case "musicbrainz":
		allowed = host == "coverartarchive.org" || host == "archive.org" || strings.HasSuffix(host, ".archive.org")
	case "apple":
		allowed = host == "mzstatic.com" || strings.HasSuffix(host, ".mzstatic.com")
	case "netease":
		allowed = host == "music.126.net" || strings.HasSuffix(host, ".music.126.net")
	case "kuwo":
		allowed = host == "kuwo.cn" || strings.HasSuffix(host, ".kuwo.cn")
	case "kugou":
		allowed = host == "kugou.com" || strings.HasSuffix(host, ".kugou.com")
	case "lrcapi":
		allowed = host == "lrc.cx" || strings.HasSuffix(host, ".lrc.cx")
	}
	if !allowed {
		return nil, fmt.Errorf("%w: host %q is not allowed for %s", ErrUnsafeArtworkURL, host, providerID)
	}
	return parsed, nil
}

func safeArtworkClient(providerID string) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		TLSHandshakeTimeout: 5 * time.Second,
		IdleConnTimeout:     15 * time.Second,
		DisableCompression:  true,
	}
	// The desktop development environment (and many user machines) reaches
	// public music CDNs through HTTPS_PROXY. A proxy connection must dial the
	// proxy host, not the provider host, so the strict direct dialer below cannot
	// be used in that case. The URL allowlist and redirect validation still
	// constrain every requested provider URL.
	if artworkProxyConfigured() {
		transport.Proxy = http.ProxyFromEnvironment
	} else {
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if _, err := validateArtworkURL(providerID, "https://"+net.JoinHostPort(host, port)); err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("%w: host has no addresses", ErrUnsafeArtworkURL)
			}
			for _, address := range addresses {
				if !publicIP(address.IP) {
					return nil, fmt.Errorf("%w: host resolves to non-public address", ErrUnsafeArtworkURL)
				}
			}
			var dialErrors []error
			for _, address := range addresses {
				connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if err == nil {
					return connection, nil
				}
				dialErrors = append(dialErrors, err)
			}
			return nil, errors.Join(dialErrors...)
		}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many provider artwork redirects")
			}
			_, err := validateArtworkURL(providerID, request.URL.String())
			return err
		},
	}
}

func artworkProxyConfigured() bool {
	request := &http.Request{URL: &url.URL{Scheme: "https", Host: "coverartarchive.org"}}
	proxy, _ := http.ProxyFromEnvironment(request)
	return proxy != nil
}

func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified()
}
