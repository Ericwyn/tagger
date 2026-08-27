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

const DefaultArchiveDownloadBaseURL = "https://archive.org"

// ArtworkDownloadOptions contains provider-owned transport settings that are
// needed after search results have been converted into short-lived artwork
// references. The Internet Archive base is intentionally separate from the
// MusicBrainz API base because Cover Art Archive discovers the concrete image
// and redirects to Internet Archive only when the image is downloaded.
type ArtworkDownloadOptions struct {
	ArchiveDownloadBaseURL string
}

// ArtworkDownloadOptionsProvider is an optional strategy capability used by
// Registry.DownloadArtwork. Keeping it separate from Configurable means other
// strategies do not need to expose artwork-specific settings.
type ArtworkDownloadOptionsProvider interface {
	ArtworkDownloadOptions() ArtworkDownloadOptions
}

func DownloadArtwork(ctx context.Context, reference ArtworkReference, client *http.Client) (artwork.Asset, error) {
	return DownloadArtworkWithOptions(ctx, reference, client, ArtworkDownloadOptions{})
}

func DownloadArtworkWithOptions(ctx context.Context, reference ArtworkReference, client *http.Client, options ArtworkDownloadOptions) (artwork.Asset, error) {
	config, err := parseArtworkDownloadConfig(reference.ProviderID, options)
	if err != nil {
		return artwork.Asset{}, err
	}
	parsed, err := validateArtworkURLWithConfig(reference.ProviderID, reference.URL, config)
	if err != nil {
		return artwork.Asset{}, err
	}
	parsed = rewriteArtworkURL(reference.ProviderID, parsed, config)
	if _, err := validateArtworkURLWithConfig(reference.ProviderID, parsed.String(), config); err != nil {
		return artwork.Asset{}, err
	}
	if client == nil {
		client = safeArtworkClient(reference.ProviderID, config)
	} else {
		client = withArtworkRedirectPolicy(client, reference.ProviderID, config)
	}
	body, err := GetBytesWithHeaders(ctx, client, parsed.String(), ArtworkUserAgent(reference.ProviderID), map[string]string{
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
	return validateArtworkURLWithConfig(providerID, value, artworkDownloadConfig{})
}

type artworkDownloadConfig struct {
	archiveDownloadBase *url.URL
}

func parseArtworkDownloadConfig(providerID string, options ArtworkDownloadOptions) (artworkDownloadConfig, error) {
	if providerID != "musicbrainz" {
		return artworkDownloadConfig{}, nil
	}
	baseURL := strings.TrimSpace(options.ArchiveDownloadBaseURL)
	if baseURL == "" {
		baseURL = DefaultArchiveDownloadBaseURL
	}
	normalized, err := ValidateArtworkDownloadBaseURL(baseURL, "archiveDownloadBaseUrl")
	if err != nil {
		return artworkDownloadConfig{}, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return artworkDownloadConfig{}, err
	}
	return artworkDownloadConfig{archiveDownloadBase: parsed}, nil
}

// ValidateArtworkDownloadBaseURL accepts an HTTPS origin with an optional path
// prefix. A path proxy such as /https/archive.org can therefore receive the
// original /download/{item}/{file} suffix. Queries and fragments remain
// disallowed because carrying them across the rewritten request is ambiguous.
func ValidateArtworkDownloadBaseURL(value, field string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("%s 必须是 HTTPS URL", field)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", fmt.Errorf("%s 不能包含查询参数或片段", field)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return (&url.URL{Scheme: "https", Host: parsed.Host, Path: parsed.Path}).String(), nil
}

func validateArtworkURLWithConfig(providerID, value string, config artworkDownloadConfig) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("%w: HTTPS provider URL required", ErrUnsafeArtworkURL)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	allowed := artworkHostAllowed(providerID, host)
	if !allowed && providerID == "musicbrainz" && config.archiveDownloadBase != nil {
		mirrorHost := strings.ToLower(strings.TrimSuffix(config.archiveDownloadBase.Hostname(), "."))
		allowed = host == mirrorHost
	}
	if !allowed {
		return nil, fmt.Errorf("%w: host %q is not allowed for %s", ErrUnsafeArtworkURL, host, providerID)
	}
	return parsed, nil
}

func artworkHostAllowed(providerID, host string) bool {
	switch providerID {
	case "musicbrainz":
		return host == "coverartarchive.org" || host == "archive.org" || strings.HasSuffix(host, ".archive.org")
	case "apple":
		return host == "mzstatic.com" || strings.HasSuffix(host, ".mzstatic.com")
	case "netease":
		return host == "music.126.net" || strings.HasSuffix(host, ".music.126.net")
	case "kuwo":
		return host == "kuwo.cn" || strings.HasSuffix(host, ".kuwo.cn")
	case "kugou":
		return host == "kugou.com" || strings.HasSuffix(host, ".kugou.com")
	case "lrcapi":
		// LrcApi is an aggregator and may return the original artwork URL
		// from one of the supported music catalogs. Keep this an explicit
		// union of known provider CDNs; never treat an arbitrary LrcApi URL
		// as safe merely because it came from the aggregator.
		return host == "lrc.cx" || strings.HasSuffix(host, ".lrc.cx") ||
			artworkHostAllowed("apple", host) ||
			artworkHostAllowed("netease", host) ||
			artworkHostAllowed("kuwo", host) ||
			artworkHostAllowed("kugou", host) ||
			artworkHostAllowed("musicbrainz", host)
	default:
		return false
	}
}

func rewriteArtworkURL(providerID string, source *url.URL, config artworkDownloadConfig) *url.URL {
	if providerID != "musicbrainz" || source == nil || config.archiveDownloadBase == nil {
		return source
	}
	host := strings.ToLower(strings.TrimSuffix(source.Hostname(), "."))
	if host != "archive.org" || !strings.HasPrefix(source.EscapedPath(), "/download/") {
		return source
	}
	rewritten := *source
	rewritten.Scheme = config.archiveDownloadBase.Scheme
	rewritten.Host = config.archiveDownloadBase.Host
	prefix := strings.TrimRight(config.archiveDownloadBase.Path, "/")
	rewritten.Path = prefix + "/" + strings.TrimLeft(source.Path, "/")
	rewritten.RawPath = ""
	return &rewritten
}

func withArtworkRedirectPolicy(client *http.Client, providerID string, config artworkDownloadConfig) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	configured := *client
	originalCheckRedirect := client.CheckRedirect
	configured.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many provider artwork redirects")
		}
		request.URL = rewriteArtworkURL(providerID, request.URL, config)
		request.Host = ""
		if _, err := validateArtworkURLWithConfig(providerID, request.URL.String(), config); err != nil {
			return err
		}
		if originalCheckRedirect != nil {
			return originalCheckRedirect(request, via)
		}
		return nil
	}
	return &configured
}

func safeArtworkClient(providerID string, config artworkDownloadConfig) *http.Client {
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
			if _, err := validateArtworkURLWithConfig(providerID, "https://"+net.JoinHostPort(host, port), config); err != nil {
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
	return withArtworkRedirectPolicy(&http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}, providerID, config)
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
