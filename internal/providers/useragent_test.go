package providers_test

import (
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/providers/itunes"
	"github.com/ericwyn/tagger/internal/providers/kugou"
	"github.com/ericwyn/tagger/internal/providers/kuwo"
	"github.com/ericwyn/tagger/internal/providers/lrcapi"
	"github.com/ericwyn/tagger/internal/providers/lrclib"
	"github.com/ericwyn/tagger/internal/providers/musicbrainz"
	"github.com/ericwyn/tagger/internal/providers/netease"
)

type configurableProvider interface {
	ConfigFields() []providers.ConfigField
	Configure(map[string]string) error
}

type resettableProvider interface {
	ResetConfig() error
}

func TestBuiltInProvidersUseSourceAppropriateDefaultUserAgents(t *testing.T) {
	cases := []struct {
		name       string
		providerID string
		strategy   providers.Strategy
		want       string
	}{
		{name: "musicbrainz", providerID: "musicbrainz", strategy: musicbrainz.New(musicbrainz.Config{}), want: providers.MusicBrainzUserAgent},
		{name: "lrclib", providerID: "lrclib", strategy: lrclib.New(lrclib.Config{}), want: providers.BrowserUserAgent},
		{name: "apple", providerID: "apple", strategy: itunes.New(itunes.Config{}), want: providers.BrowserUserAgent},
		{name: "netease", providerID: "netease", strategy: netease.New(netease.Config{}), want: providers.BrowserUserAgent},
		{name: "kuwo", providerID: "kuwo", strategy: kuwo.New(kuwo.Config{}), want: providers.BrowserUserAgent},
		{name: "kugou", providerID: "kugou", strategy: kugou.New(kugou.Config{}), want: providers.MobileBrowserUserAgent},
		{name: "lrcapi", providerID: "lrcapi", strategy: lrcapi.New(lrcapi.Config{}), want: providers.BrowserUserAgent},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			configurable, ok := testCase.strategy.(configurableProvider)
			if !ok {
				t.Fatal("provider does not expose configuration")
			}
			field := userAgentField(configurable.ConfigFields())
			if field.Value != testCase.want {
				t.Fatalf("default user-agent = %q, want %q", field.Value, testCase.want)
			}
			if testCase.providerID != "musicbrainz" && strings.Contains(strings.ToLower(field.Value), "tagger") {
				t.Fatalf("web provider leaked project identity in default user-agent %q", field.Value)
			}
		})
	}
}

func TestBuiltInProviderResetRestoresUserAgentAndEndpoints(t *testing.T) {
	cases := []struct {
		name     string
		strategy providers.Strategy
		want     string
	}{
		{name: "musicbrainz", strategy: musicbrainz.New(musicbrainz.Config{}), want: providers.MusicBrainzUserAgent},
		{name: "lrclib", strategy: lrclib.New(lrclib.Config{}), want: providers.BrowserUserAgent},
		{name: "apple", strategy: itunes.New(itunes.Config{}), want: providers.BrowserUserAgent},
		{name: "netease", strategy: netease.New(netease.Config{}), want: providers.BrowserUserAgent},
		{name: "kuwo", strategy: kuwo.New(kuwo.Config{}), want: providers.BrowserUserAgent},
		{name: "kugou", strategy: kugou.New(kugou.Config{}), want: providers.MobileBrowserUserAgent},
		{name: "lrcapi", strategy: lrcapi.New(lrcapi.Config{}), want: providers.BrowserUserAgent},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			configurable := testCase.strategy.(configurableProvider)
			resettable := testCase.strategy.(resettableProvider)
			if err := configurable.Configure(map[string]string{"userAgent": "Tagger/legacy"}); err != nil {
				t.Fatal(err)
			}
			if err := resettable.ResetConfig(); err != nil {
				t.Fatal(err)
			}
			if got := userAgentField(configurable.ConfigFields()).Value; got != testCase.want {
				t.Fatalf("reset user-agent = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestLegacyTaggerUserAgentDetection(t *testing.T) {
	for _, value := range []string{"Tagger/dev (https://github.com/ericwyn/tagger)", "Tagger/0.1 (experimental netease adapter)", "Tagger/0.1 (+https://github.com/ericwyn/tagger)"} {
		if !providers.IsLegacyTaggerUserAgent(value) {
			t.Errorf("IsLegacyTaggerUserAgent(%q) = false", value)
		}
	}
	for _, value := range []string{"Mozilla/5.0 Chrome/131", "MyMusicClient/2.0 (https://example.test)"} {
		if providers.IsLegacyTaggerUserAgent(value) {
			t.Errorf("IsLegacyTaggerUserAgent(%q) = true", value)
		}
	}
}

func userAgentField(fields []providers.ConfigField) providers.ConfigField {
	for _, field := range fields {
		if field.Key == "userAgent" {
			return field
		}
	}
	return providers.ConfigField{}
}
