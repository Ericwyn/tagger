package domain

type BatchEditMode string

const (
	BatchEditSet     BatchEditMode = "set"
	BatchEditAppend  BatchEditMode = "append"
	BatchEditDelete  BatchEditMode = "delete"
	BatchEditReplace BatchEditMode = "replace"
)

type BatchEditOperation struct {
	Field string        `json:"field"`
	Mode  BatchEditMode `json:"mode"`
	Value string        `json:"value,omitempty"`
	Find  string        `json:"find,omitempty"`
}

type BatchEditItem struct {
	TrackID      string `json:"trackId"`
	BaseRevision string `json:"baseRevision"`
}

type BatchEditPayload struct {
	Items          []BatchEditItem      `json:"items"`
	Operations     []BatchEditOperation `json:"operations"`
	SequenceTracks bool                 `json:"sequenceTracks"`
}
