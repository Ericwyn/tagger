package domain

// OrganizeItemRequest identifies a track selected for a file-location job.
// BaseRevision prevents a stale preview from moving a file that changed while
// the user was reviewing the proposed paths.
type OrganizeItemRequest struct {
	TrackID      string `json:"trackId"`
	BaseRevision string `json:"baseRevision"`
}

// OrganizeMode controls the directory hierarchy used for a file-location job.
// The original artist/album layout remains the default for backwards
// compatibility with jobs created before mode selection was added.
type OrganizeMode string

const (
	OrganizeModeArtistAlbum OrganizeMode = "artist_album"
	OrganizeModeArtist      OrganizeMode = "artist"
)

const DefaultOrganizeMode = OrganizeModeArtistAlbum

func (mode OrganizeMode) Valid() bool {
	return mode == OrganizeModeArtistAlbum || mode == OrganizeModeArtist
}

func NormalizeOrganizeMode(mode OrganizeMode) OrganizeMode {
	if mode == "" {
		return DefaultOrganizeMode
	}
	return mode
}

type OrganizePayload struct {
	Items             []OrganizeItemRequest `json:"items"`
	MoveLyricsSidecar bool                  `json:"moveLyricsSidecar"`
	Mode              OrganizeMode          `json:"mode,omitempty"`
	BasePath          string                `json:"basePath,omitempty"`
}

type OrganizeItemState string

const (
	OrganizeReady    OrganizeItemState = "ready"
	OrganizeNoop     OrganizeItemState = "noop"
	OrganizeConflict OrganizeItemState = "conflict"
	OrganizeInvalid  OrganizeItemState = "invalid"
	OrganizeMoved    OrganizeItemState = "moved"
	OrganizeFailed   OrganizeItemState = "failed"
)

type OrganizeItem struct {
	ID            string            `json:"id,omitempty"`
	JobID         string            `json:"jobId,omitempty"`
	TrackID       string            `json:"trackId"`
	Source        string            `json:"source"`
	Target        string            `json:"target"`
	PrimaryArtist string            `json:"primaryArtist"`
	Album         string            `json:"album"`
	SidecarSource string            `json:"sidecarSource,omitempty"`
	SidecarTarget string            `json:"sidecarTarget,omitempty"`
	SidecarExists bool              `json:"sidecarExists,omitempty"`
	State         OrganizeItemState `json:"state"`
	Error         string            `json:"error,omitempty"`
	Warnings      []string          `json:"warnings,omitempty"`
	UpdatedAt     string            `json:"updatedAt,omitempty"`
}
