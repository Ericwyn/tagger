package domain

import "time"

type RevisionDiff struct {
	Field     string    `json:"field"`
	Operation Operation `json:"operation"`
	Before    any       `json:"before"`
	After     any       `json:"after"`
}

type Revision struct {
	ID                string              `json:"id"`
	LibraryID         string              `json:"libraryId"`
	TrackID           string              `json:"trackId"`
	TrackTitle        string              `json:"trackTitle"`
	FileName          string              `json:"fileName"`
	Action            string              `json:"action"`
	Source            string              `json:"source"`
	BaseRevision      string              `json:"baseRevision"`
	ResultRevision    string              `json:"resultRevision"`
	Fields            []string            `json:"fields"`
	Diff              []RevisionDiff      `json:"diff"`
	CoverTone         CoverTone           `json:"coverTone"`
	CreatedAt         time.Time           `json:"createdAt"`
	BeforeTags        map[string][]string `json:"-"`
	AfterTags         map[string][]string `json:"-"`
	BeforeArtwork     *ArtworkSnapshot    `json:"beforeArtwork,omitempty"`
	AfterArtwork      *ArtworkSnapshot    `json:"afterArtwork,omitempty"`
	BeforeArtworkHash string              `json:"-"`
	AfterArtworkHash  string              `json:"-"`
	BeforeSidecar     *SidecarSnapshot    `json:"beforeSidecar,omitempty"`
	AfterSidecar      *SidecarSnapshot    `json:"afterSidecar,omitempty"`
}

// ArtworkSnapshot is a validated embedded image captured for a revision. Data
// is intentionally omitted from JSON responses; it is loaded only when a
// restore operation needs the blob bytes.
type ArtworkSnapshot struct {
	MIME   string `json:"mime"`
	Format string `json:"format"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size"`
	Hash   string `json:"hash"`
	Data   []byte `json:"-"`
}

// SidecarSnapshot is the auditable state of a same-basename lyrics file.
// Content is retained in the local revision store for restore, but is never
// included in API JSON responses by the server's revision projection.
type SidecarSnapshot struct {
	Exists     bool   `json:"exists"`
	Revision   string `json:"revision,omitempty"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
	Content    string `json:"content,omitempty"`
}
