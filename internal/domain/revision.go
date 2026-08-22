package domain

import "time"

type RevisionDiff struct {
	Field     string    `json:"field"`
	Operation Operation `json:"operation"`
	Before    any       `json:"before"`
	After     any       `json:"after"`
}

type Revision struct {
	ID             string              `json:"id"`
	LibraryID      string              `json:"libraryId"`
	TrackID        string              `json:"trackId"`
	TrackTitle     string              `json:"trackTitle"`
	FileName       string              `json:"fileName"`
	Action         string              `json:"action"`
	Source         string              `json:"source"`
	BaseRevision   string              `json:"baseRevision"`
	ResultRevision string              `json:"resultRevision"`
	Fields         []string            `json:"fields"`
	Diff           []RevisionDiff      `json:"diff"`
	CoverTone      CoverTone           `json:"coverTone"`
	CreatedAt      time.Time           `json:"createdAt"`
	BeforeTags     map[string][]string `json:"-"`
	AfterTags      map[string][]string `json:"-"`
}
