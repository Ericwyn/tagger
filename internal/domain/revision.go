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
