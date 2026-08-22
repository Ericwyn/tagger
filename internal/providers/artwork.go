package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return artwork.Asset{}, err
	}
	request.Header.Set("Accept", "image/jpeg, image/png, image/webp")
	request.Header.Set("User-Agent", "Tagger/0.1 (+https://github.com/ericwyn/tagger)")
	response, err := client.Do(request)
	if err != nil {
		return artwork.Asset{}, fmt.Errorf("download provider artwork: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return artwork.Asset{}, fmt.Errorf("provider artwork HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, artwork.MaxBytes+1))
	if err != nil {
		return artwork.Asset{}, fmt.Errorf("read provider artwork: %w", err)
	}
	if len(body) > artwork.MaxBytes {
		return artwork.Asset{}, fmt.Errorf("provider artwork exceeds %d MiB", artwork.MaxBytes>>20)
	}
	asset, err := artwork.Validate(body, response.Header.Get("Content-Type"))
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
	}
	if !allowed {
		return nil, fmt.Errorf("%w: host %q is not allowed for %s", ErrUnsafeArtworkURL, host, providerID)
	}
	return parsed, nil
}

func safeArtworkClient(providerID string) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
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
		},
		TLSHandshakeTimeout: 5 * time.Second,
		IdleConnTimeout:     15 * time.Second,
		DisableCompression:  true,
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

func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified()
}
