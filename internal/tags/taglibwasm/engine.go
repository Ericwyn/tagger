package taglibwasm

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/tags"
	"go.senan.xyz/taglib"
)

type Engine struct{}

func New() *Engine { return &Engine{} }

func (e *Engine) Read(ctx context.Context, path string) (tags.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return tags.Snapshot{}, err
	}

	raw, err := taglib.ReadTags(path)
	if err != nil {
		return tags.Snapshot{}, fmt.Errorf("read tags: %w", err)
	}
	properties, err := taglib.ReadProperties(path)
	if err != nil {
		return tags.Snapshot{}, fmt.Errorf("read properties: %w", err)
	}

	container := normalizeContainer(properties.Format)
	codec := strings.ToUpper(properties.InnerCodec)
	if codec == "" {
		switch container {
		case "MPEG":
			codec = "MP3"
		case "WAV":
			codec = "PCM"
		default:
			codec = container
		}
	}

	return tags.Snapshot{
		Raw:             raw,
		DurationSeconds: int64(properties.Length.Round(0).Seconds()),
		Properties: domain.TrackProperties{
			Container:    container,
			Codec:        codec,
			BitrateKbps:  int(properties.BitRate),
			SampleRateHz: int(properties.SampleRate),
			BitDepth:     int(properties.BitDepth),
			Channels:     int(properties.Channels),
		},
		ArtworkCount: len(properties.Images),
	}, nil
}

func (e *Engine) Version() string { return "go-taglib/v0.14.0 (TagLib 2.1.1)" }

func normalizeContainer(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mpeg", "mp3":
		return "MPEG"
	case "flac":
		return "FLAC"
	case "wav", "wave":
		return "WAV"
	default:
		return strings.ToUpper(format)
	}
}
