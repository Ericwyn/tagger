package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

const MaxSidecarLyricsBytes = 1 << 20

// SidecarInfo is the lightweight sidecar projection included in a track
// response. The content itself is read on demand so the library list stays
// small.
type SidecarInfo struct {
	Exists     bool   `json:"exists"`
	Revision   string `json:"revision,omitempty"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
}

// SidecarRevision identifies the exact LRC content used by the optimistic
// sidecar write guard. It deliberately does not depend on filesystem mtime.
func SidecarRevision(content []byte) string {
	hash := sha256.Sum256(content)
	return "sidecar-" + hex.EncodeToString(hash[:12])
}
