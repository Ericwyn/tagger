package domain

// TrackFormat is the set of formats certified by the first product version.
type TrackFormat string

const (
	FormatMP3  TrackFormat = "mp3"
	FormatFLAC TrackFormat = "flac"
	FormatWAV  TrackFormat = "wav"
)

type TrackHealth string

const (
	HealthComplete       TrackHealth = "complete"
	HealthMissingArtwork TrackHealth = "missing-artwork"
	HealthMissingLyrics  TrackHealth = "missing-lyrics"
	HealthNeedsReview    TrackHealth = "needs-review"
	HealthParseError     TrackHealth = "parse-error"
)

type CoverTone string

const (
	CoverVermilion CoverTone = "vermilion"
	CoverMoss      CoverTone = "moss"
	CoverCobalt    CoverTone = "cobalt"
	CoverSand      CoverTone = "sand"
	CoverCharcoal  CoverTone = "charcoal"
	CoverJade      CoverTone = "jade"
)

type TrackProperties struct {
	Container    string `json:"container"`
	Codec        string `json:"codec"`
	BitrateKbps  int    `json:"bitrateKbps"`
	SampleRateHz int    `json:"sampleRateHz"`
	BitDepth     int    `json:"bitDepth"`
	Channels     int    `json:"channels"`
}

// Track is the lightweight normalized projection consumed by the library UI.
// Raw tags and binary artwork deliberately live behind separate endpoints.
type Track struct {
	ID                   string          `json:"id"`
	FileName             string          `json:"fileName"`
	RelativePath         string          `json:"relativePath"`
	FolderID             string          `json:"folderId"`
	Format               TrackFormat     `json:"format"`
	SizeBytes            int64           `json:"sizeBytes"`
	DurationSeconds      int64           `json:"durationSeconds"`
	Title                string          `json:"title"`
	Artists              []string        `json:"artists"`
	Album                string          `json:"album"`
	AlbumArtists         []string        `json:"albumArtists"`
	TrackNumber          *int            `json:"trackNumber,omitempty"`
	TrackTotal           *int            `json:"trackTotal,omitempty"`
	DiscNumber           *int            `json:"discNumber,omitempty"`
	DiscTotal            *int            `json:"discTotal,omitempty"`
	Year                 *int            `json:"year,omitempty"`
	Genres               []string        `json:"genres"`
	Lyrics               string          `json:"lyrics"`
	Comment              string          `json:"comment"`
	Composers            []string        `json:"composers"`
	Conductor            string          `json:"conductor"`
	Lyricists            []string        `json:"lyricists"`
	Copyright            string          `json:"copyright"`
	BPM                  *int            `json:"bpm,omitempty"`
	ISRC                 string          `json:"isrc"`
	MusicBrainzTrackID   string          `json:"musicbrainzTrackId"`
	MusicBrainzReleaseID string          `json:"musicbrainzReleaseId"`
	MusicBrainzArtistIDs []string        `json:"musicbrainzArtistIds"`
	AcoustID             string          `json:"acoustidId"`
	AcoustIDFingerprint  string          `json:"acoustidFingerprint"`
	LyricsSidecar        *SidecarInfo    `json:"lyricsSidecar,omitempty"`
	ArtworkCount         int             `json:"artworkCount"`
	ArtworkWidth         int             `json:"artworkWidth,omitempty"`
	ArtworkHeight        int             `json:"artworkHeight,omitempty"`
	ArtworkSizeBytes     int64           `json:"artworkSizeBytes,omitempty"`
	CoverTone            CoverTone       `json:"coverTone"`
	Health               TrackHealth     `json:"health"`
	Properties           TrackProperties `json:"properties"`
	Writable             bool            `json:"writable"`
	Revision             string          `json:"revision"`
	ModifiedAt           string          `json:"modifiedAt"`
	ParseError           string          `json:"parseError,omitempty"`
}

type FolderNode struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Count    int          `json:"count"`
	ParentID string       `json:"parentId,omitempty"`
	Children []FolderNode `json:"children,omitempty"`
}

type LibrarySummary struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	RootLabel     string       `json:"rootLabel"`
	RootPath      string       `json:"rootPath,omitempty"`
	TrackCount    int          `json:"trackCount"`
	FolderCount   int          `json:"folderCount"`
	Writable      bool         `json:"writable"`
	LastScanLabel string       `json:"lastScanLabel"`
	Folders       []FolderNode `json:"folders"`
}

type ScanReport struct {
	StartedAt    string   `json:"startedAt"`
	CompletedAt  string   `json:"completedAt"`
	Discovered   int      `json:"discovered"`
	Parsed       int      `json:"parsed"`
	Failed       int      `json:"failed"`
	WarningCount int      `json:"warningCount"`
	Warnings     []string `json:"warnings,omitempty"`
}
