package providers_test

import (
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

func TestBuiltInProvidersExposeProxyForAPIAndArtwork(t *testing.T) {
	factories := []func() providers.Strategy{
		func() providers.Strategy { return musicbrainz.New(musicbrainz.Config{}) },
		func() providers.Strategy { return lrclib.New(lrclib.Config{}) },
		func() providers.Strategy { return itunes.New(itunes.Config{}) },
		func() providers.Strategy { return netease.New(netease.Config{}) },
		func() providers.Strategy { return kugou.New(kugou.Config{}) },
		func() providers.Strategy { return kuwo.New(kuwo.Config{}) },
		func() providers.Strategy { return lrcapi.New(lrcapi.Config{}) },
	}
	for _, factory := range factories {
		strategy := factory()
		t.Run(strategy.Descriptor().ID, func(t *testing.T) {
			configurable, ok := strategy.(providers.Configurable)
			if !ok {
				t.Fatal("built-in provider is not configurable")
			}
			var proxyField providers.ConfigField
			for _, field := range configurable.ConfigFields() {
				if field.Key == "proxyUrl" {
					proxyField = field
					break
				}
			}
			if proxyField.Type != "url" || proxyField.Required || proxyField.Value != "" {
				t.Fatalf("default proxy field = %#v", proxyField)
			}
			var simplifyField providers.ConfigField
			for _, field := range configurable.ConfigFields() {
				if field.Key == "simplifyChinese" {
					simplifyField = field
					break
				}
			}
			if simplifyField.Type != "boolean" || simplifyField.Value != "false" {
				t.Fatalf("default simplify field = %#v", simplifyField)
			}
			if err := configurable.Configure(map[string]string{"simplifyChinese": "true"}); err != nil {
				t.Fatal(err)
			}
			for _, field := range configurable.ConfigFields() {
				if field.Key == "simplifyChinese" && field.Value != "true" {
					t.Fatalf("configured simplify field = %#v", field)
				}
			}
			if err := configurable.Configure(map[string]string{"proxyUrl": "http://127.0.0.1:7890/"}); err != nil {
				t.Fatal(err)
			}
			artworkOptions, ok := strategy.(providers.ArtworkDownloadOptionsProvider)
			if !ok {
				t.Fatal("built-in provider does not share transport options with artwork")
			}
			options := artworkOptions.ArtworkDownloadOptions()
			if options.ProxyURL != "http://127.0.0.1:7890" || options.Gate == nil {
				t.Fatalf("configured artwork options = %#v", options)
			}
			resetter, ok := strategy.(providers.ConfigResetter)
			if !ok {
				t.Fatal("built-in provider cannot reset configuration")
			}
			if err := resetter.ResetConfig(); err != nil {
				t.Fatal(err)
			}
			if reset := artworkOptions.ArtworkDownloadOptions(); reset.ProxyURL != "" || reset.Gate != options.Gate {
				t.Fatalf("reset artwork options = %#v", reset)
			}
			for _, field := range configurable.ConfigFields() {
				if field.Key == "simplifyChinese" && field.Value != "false" {
					t.Fatalf("reset simplify field = %#v", field)
				}
			}
		})
	}
}
