package providers

import "strings"

// BrowserUserAgent is used for providers whose public endpoints are also
// consumed by their web clients. It intentionally identifies a normal,
// standards-compliant browser instead of leaking the name of the proxy
// application into every request.
const BrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// MobileBrowserUserAgent is useful for endpoints exposed specifically for a
// mobile web client (for example, KuGou's mobile search API). It is still a
// browser identity rather than a forged first-party native-app identity.
const MobileBrowserUserAgent = "Mozilla/5.0 (Linux; Android 13; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36"

// MusicBrainzUserAgent remains descriptive because MusicBrainz explicitly
// requires an application name/version and a contact URL. Its API also
// enforces a one-request-per-second policy, which the provider gate handles.
const MusicBrainzUserAgent = "Tagger/0.1 (https://github.com/ericwyn/tagger)"

// DefaultUserAgent returns the least surprising default for a provider. The
// caller may still override it in the provider configuration panel.
func DefaultUserAgent(providerID string) string {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "musicbrainz":
		return MusicBrainzUserAgent
	case "kugou":
		return MobileBrowserUserAgent
	default:
		return BrowserUserAgent
	}
}

// IsLegacyTaggerUserAgent identifies defaults emitted by older Tagger builds.
// It is deliberately narrow enough to leave a user-supplied non-Tagger UA
// untouched while allowing startup to migrate the old built-in defaults.
func IsLegacyTaggerUserAgent(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || !strings.Contains(value, "tagger") {
		return false
	}
	return strings.HasPrefix(value, "tagger/") || strings.Contains(value, "experimental ") || strings.Contains(value, "optional lrcapi")
}

// ArtworkUserAgent is sent to provider CDNs by the safe artwork downloader.
// Image CDNs do not need the MusicBrainz application identity and use the
// same ordinary browser profile as web-facing provider endpoints.
func ArtworkUserAgent(_ string) string {
	return BrowserUserAgent
}
