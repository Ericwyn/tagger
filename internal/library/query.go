package library

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ericwyn/tagger/internal/domain"
)

const (
	DefaultTrackPageSize = 100
	MaxTrackPageSize     = 200
	MaxTrackResolveSize  = 1000
)

type TrackSort string

const (
	TrackSortAlbum    TrackSort = "album"
	TrackSortTitle    TrackSort = "title"
	TrackSortModified TrackSort = "modified"
	TrackSortFormat   TrackSort = "format"
)

// TrackQuery is shared by paginated browsing and bounded batch selection.
// FolderPath uses the same display path as FolderNode ("Artist · Album").
type TrackQuery struct {
	Query             string             `json:"q,omitempty"`
	FolderID          string             `json:"folderId,omitempty"`
	FolderPath        string             `json:"folderPath,omitempty"`
	IncludeSubfolders bool               `json:"includeSubfolders,omitempty"`
	Health            domain.TrackHealth `json:"health,omitempty"`
	Format            domain.TrackFormat `json:"format,omitempty"`
	Sort              TrackSort          `json:"sort,omitempty"`
}

type TrackPage struct {
	Tracks     []domain.Track `json:"tracks"`
	Total      int            `json:"total"`
	NextCursor string         `json:"nextCursor,omitempty"`
	HasMore    bool           `json:"hasMore"`
}

var (
	ErrInvalidTrackQuery   = errors.New("invalid track query")
	ErrInvalidTrackCursor  = errors.New("invalid track cursor")
	ErrStaleTrackCursor    = errors.New("stale track cursor")
	ErrTrackSelectionLarge = errors.New("track selection exceeds limit")
)

type trackCursor struct {
	Generation uint64 `json:"generation"`
	QueryHash  string `json:"queryHash"`
	Offset     int    `json:"offset"`
}

func NormalizeTrackQuery(query TrackQuery) (TrackQuery, error) {
	query.Query = strings.TrimSpace(query.Query)
	query.FolderID = strings.TrimSpace(query.FolderID)
	query.FolderPath = strings.TrimSpace(strings.Trim(query.FolderPath, "· "))
	query.Health = domain.TrackHealth(strings.TrimSpace(string(query.Health)))
	query.Format = domain.TrackFormat(strings.TrimSpace(string(query.Format)))
	query.Sort = TrackSort(strings.TrimSpace(string(query.Sort)))
	if query.Sort == "" {
		query.Sort = TrackSortAlbum
	}
	switch query.Sort {
	case TrackSortAlbum, TrackSortTitle, TrackSortModified, TrackSortFormat:
	default:
		return TrackQuery{}, fmt.Errorf("%w: unsupported sort %q", ErrInvalidTrackQuery, query.Sort)
	}
	if query.Health != "" {
		switch query.Health {
		case domain.HealthComplete, domain.HealthMissingArtwork, domain.HealthMissingLyrics, domain.HealthNeedsReview, domain.HealthParseError, domain.HealthMissing:
		default:
			return TrackQuery{}, fmt.Errorf("%w: unsupported health %q", ErrInvalidTrackQuery, query.Health)
		}
	}
	if query.Format != "" {
		switch query.Format {
		case domain.FormatMP3, domain.FormatFLAC, domain.FormatWAV:
		default:
			return TrackQuery{}, fmt.Errorf("%w: unsupported format %q", ErrInvalidTrackQuery, query.Format)
		}
	}
	return query, nil
}

func queryHash(query TrackQuery) string {
	encoded, _ := json.Marshal(query)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func encodeTrackCursor(cursor trackCursor) string {
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeTrackCursor(value string) (trackCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return trackCursor{}, fmt.Errorf("%w: base64", ErrInvalidTrackCursor)
	}
	var cursor trackCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Generation == 0 || cursor.QueryHash == "" || cursor.Offset < 0 {
		return trackCursor{}, fmt.Errorf("%w: payload", ErrInvalidTrackCursor)
	}
	return cursor, nil
}

func normalizePageSize(limit int) (int, error) {
	if limit == 0 {
		return DefaultTrackPageSize, nil
	}
	if limit < 1 || limit > MaxTrackPageSize {
		return 0, fmt.Errorf("%w: page size must be between 1 and %d", ErrInvalidTrackQuery, MaxTrackPageSize)
	}
	return limit, nil
}

func trackMatchesQuery(track domain.Track, query TrackQuery) bool {
	if query.Health == domain.HealthMissing {
		if !track.Missing {
			return false
		}
	} else if track.Missing {
		return false
	}
	if query.FolderID != "" && track.FolderID != query.FolderID {
		return false
	}
	if query.FolderPath != "" {
		directory := filepath.ToSlash(filepath.Dir(track.RelativePath))
		folderPath := strings.ReplaceAll(query.FolderPath, " · ", "/")
		if directory != folderPath && (!query.IncludeSubfolders || !strings.HasPrefix(directory, folderPath+"/")) {
			return false
		}
	}
	if query.Health != "" && query.Health != domain.HealthMissing {
		if track.SyncState == domain.SyncDraft || track.Health != query.Health {
			return false
		}
	}
	if query.Format != "" && track.Format != query.Format {
		return false
	}
	needle := strings.ToLower(query.Query)
	if needle == "" {
		return true
	}
	values := []string{track.Title, track.FileName, track.RelativePath, track.Album}
	values = append(values, track.Artists...)
	values = append(values, track.AlbumArtists...)
	values = append(values, track.Genres...)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func compareTrack(left, right domain.Track, mode TrackSort) int {
	compareText := func(a, b string) int {
		a = strings.ToLower(strings.TrimSpace(a))
		b = strings.ToLower(strings.TrimSpace(b))
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	}
	comparePath := func() int { return compareText(left.RelativePath, right.RelativePath) }
	switch mode {
	case TrackSortTitle:
		if result := compareText(firstTrackText(left.Title, left.FileName), firstTrackText(right.Title, right.FileName)); result != 0 {
			return result
		}
	case TrackSortModified:
		if left.FileFingerprint.ModifiedUnixNano != right.FileFingerprint.ModifiedUnixNano {
			if left.FileFingerprint.ModifiedUnixNano > right.FileFingerprint.ModifiedUnixNano {
				return -1
			}
			return 1
		}
		if result := compareText(right.ModifiedAt, left.ModifiedAt); result != 0 {
			return result
		}
	case TrackSortFormat:
		if result := compareText(string(left.Format), string(right.Format)); result != 0 {
			return result
		}
		if result := compareText(firstTrackText(left.Title, left.FileName), firstTrackText(right.Title, right.FileName)); result != 0 {
			return result
		}
	default:
		if result := compareText(left.Album, right.Album); result != 0 {
			return result
		}
		if result := compareOptionalInt(left.DiscNumber, right.DiscNumber); result != 0 {
			return result
		}
		if result := compareOptionalInt(left.TrackNumber, right.TrackNumber); result != 0 {
			return result
		}
		if result := compareText(firstTrackText(left.Title, left.FileName), firstTrackText(right.Title, right.FileName)); result != 0 {
			return result
		}
	}
	if result := comparePath(); result != 0 {
		return result
	}
	return compareText(left.ID, right.ID)
}

func firstTrackText(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func compareOptionalInt(left, right *int) int {
	leftValue, rightValue := 0, 0
	if left != nil {
		leftValue = *left
	}
	if right != nil {
		rightValue = *right
	}
	if leftValue < rightValue {
		return -1
	}
	if leftValue > rightValue {
		return 1
	}
	return 0
}

func sortTracks(tracks []domain.Track, mode TrackSort) {
	sort.SliceStable(tracks, func(i, j int) bool { return compareTrack(tracks[i], tracks[j], mode) < 0 })
}
