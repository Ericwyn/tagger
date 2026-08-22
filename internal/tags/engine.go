package tags

import (
	"context"

	"github.com/ericwyn/tagger/internal/domain"
)

// Snapshot is a lossless property map plus the technical information needed
// by the library projection. The scanner performs product-level normalization.
type Snapshot struct {
	Raw             map[string][]string
	Properties      domain.TrackProperties
	DurationSeconds int64
	ArtworkCount    int
}

// Engine keeps the rest of the application independent from TagLib and its
// property names. Write operations will be added behind the same boundary.
type Engine interface {
	Read(ctx context.Context, path string) (Snapshot, error)
	// Write updates only the provided property-map keys. An empty value list
	// deletes that key; unknown and unmentioned keys must be preserved.
	Write(ctx context.Context, path string, updates map[string][]string) error
	Version() string
}

// ArtworkEngine is an optional capability implemented by engines that can
// round-trip embedded image bytes. Keeping it separate lets lightweight test
// engines and future read-only engines continue to implement Engine only.
type ArtworkEngine interface {
	ReadArtwork(ctx context.Context, path string, index int) ([]byte, error)
	WriteArtwork(ctx context.Context, path string, index int, image []byte, mimeType string) error
}
