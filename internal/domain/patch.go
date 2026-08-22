package domain

type Operation string

const (
	OperationKeep   Operation = "keep"
	OperationSet    Operation = "set"
	OperationDelete Operation = "delete"
	OperationMerge  Operation = "merge"
)

type StringFieldPatch struct {
	Op    Operation `json:"op"`
	Value string    `json:"value,omitempty"`
}

type StringsFieldPatch struct {
	Op    Operation `json:"op"`
	Value []string  `json:"value,omitempty"`
}

type IntFieldPatch struct {
	Op    Operation `json:"op"`
	Value int       `json:"value,omitempty"`
}

// TagPatch uses pointers to distinguish an omitted field (keep) from an
// explicit delete. Each operation maps to one or more TagLib property keys.
type TagPatch struct {
	Title        *StringFieldPatch  `json:"title,omitempty"`
	Artists      *StringsFieldPatch `json:"artists,omitempty"`
	Album        *StringFieldPatch  `json:"album,omitempty"`
	AlbumArtists *StringsFieldPatch `json:"albumArtists,omitempty"`
	TrackNumber  *IntFieldPatch     `json:"trackNumber,omitempty"`
	TrackTotal   *IntFieldPatch     `json:"trackTotal,omitempty"`
	DiscNumber   *IntFieldPatch     `json:"discNumber,omitempty"`
	DiscTotal    *IntFieldPatch     `json:"discTotal,omitempty"`
	Year         *IntFieldPatch     `json:"year,omitempty"`
	Genres       *StringsFieldPatch `json:"genres,omitempty"`
	Lyrics       *StringFieldPatch  `json:"lyrics,omitempty"`
}
