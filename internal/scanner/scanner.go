package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/tags"
)

type Options struct {
	Root        string
	LibraryID   string
	LibraryName string
	Workers     int
	Now         func() time.Time
}

type Result struct {
	Library domain.LibrarySummary
	Tracks  []domain.Track
	Report  domain.ScanReport
}

type Scanner struct {
	engine tags.Engine
	opts   Options
}

func New(engine tags.Engine, opts Options) (*Scanner, error) {
	if engine == nil {
		return nil, fmt.Errorf("tag engine is required")
	}
	root, err := filepath.Abs(strings.TrimSpace(opts.Root))
	if err != nil {
		return nil, fmt.Errorf("resolve library root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat library root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("library root is not a directory: %s", root)
	}

	opts.Root = root
	if opts.LibraryID == "" {
		opts.LibraryID = "lib-" + shortHash(root)
	}
	if opts.LibraryName == "" {
		opts.LibraryName = filepath.Base(root)
	}
	if opts.Workers <= 0 {
		opts.Workers = min(runtime.NumCPU(), 8)
	}
	if opts.Workers > 32 {
		opts.Workers = 32
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Scanner{engine: engine, opts: opts}, nil
}

func (s *Scanner) Scan(ctx context.Context) (Result, error) {
	started := s.opts.Now()
	paths, warnings, err := s.discover(ctx)
	if err != nil {
		return Result{}, err
	}

	type extraction struct {
		track  domain.Track
		failed bool
	}
	jobs := make(chan string)
	results := make(chan extraction, len(paths))
	var workers sync.WaitGroup

	for range s.opts.Workers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for path := range jobs {
				track, readErr := s.extract(ctx, path)
				results <- extraction{track: track, failed: readErr != nil}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, path := range paths {
			select {
			case jobs <- path:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	tracks := make([]domain.Track, 0, len(paths))
	failed := 0
	for result := range results {
		tracks = append(tracks, result.track)
		if result.failed {
			failed++
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].RelativePath < tracks[j].RelativePath })

	completed := s.opts.Now()
	folders := buildFolders(tracks)
	rootInfo, _ := os.Stat(s.opts.Root)
	rootWritable := rootInfo != nil && rootInfo.Mode().Perm()&0o222 != 0
	return Result{
		Library: domain.LibrarySummary{
			ID:            s.opts.LibraryID,
			Name:          s.opts.LibraryName,
			RootLabel:     filepath.Base(s.opts.Root),
			TrackCount:    len(tracks),
			FolderCount:   len(folders),
			Writable:      rootWritable,
			LastScanLabel: completed.Format("2006-01-02 15:04"),
			Folders:       folders,
		},
		Tracks: tracks,
		Report: domain.ScanReport{
			StartedAt:    started.Format(time.RFC3339),
			CompletedAt:  completed.Format(time.RFC3339),
			Discovered:   len(paths),
			Parsed:       len(paths) - failed,
			Failed:       failed,
			WarningCount: len(warnings),
			Warnings:     warnings,
		},
	}, nil
}

func (s *Scanner) Root() string { return s.opts.Root }

// ScanTrack re-reads one already-indexed relative path without walking the
// rest of the library. Callers must provide a relative, non-symlinked path;
// the same format and metadata normalization as a full scan is used.
func (s *Scanner) ScanTrack(ctx context.Context, relativePath string) (domain.Track, error) {
	relativePath = filepath.ToSlash(strings.TrimSpace(relativePath))
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if relativePath == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return domain.Track{}, fmt.Errorf("invalid relative track path")
	}
	absolutePath := filepath.Join(s.opts.Root, clean)
	info, err := os.Lstat(absolutePath)
	if err != nil {
		return domain.Track{}, err
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return domain.Track{}, fmt.Errorf("track path is not a regular audio file")
	}
	if !isSupportedAudio(absolutePath) {
		return domain.Track{}, fmt.Errorf("unsupported audio format: %s", filepath.Ext(absolutePath))
	}
	return s.extract(ctx, absolutePath)
}

func (s *Scanner) discover(ctx context.Context) ([]string, []string, error) {
	paths := make([]string, 0, 256)
	warnings := make([]string, 0)
	err := filepath.WalkDir(s.opts.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			warnings = append(warnings, walkErr.Error())
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path != s.opts.Root && entry.IsDir() && isIgnoredDirectory(entry.Name()) {
			return fs.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		// Safe writers use hidden same-directory files such as
		// .song.tagger-123.mp3. Hidden files are never library entries, so a
		// concurrent scan cannot accidentally index an in-flight copy.
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if isSupportedAudio(path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, warnings, fmt.Errorf("walk library: %w", err)
	}
	sort.Strings(paths)
	return paths, warnings, nil
}

func (s *Scanner) extract(ctx context.Context, path string) (domain.Track, error) {
	info, statErr := os.Stat(path)
	relativePath, _ := filepath.Rel(s.opts.Root, path)
	relativePath = filepath.ToSlash(relativePath)
	format := formatFromPath(path)
	track := fallbackTrack(relativePath, format)
	track.ID = "trk-" + shortHash(relativePath)
	track.FileName = filepath.Base(path)
	track.RelativePath = relativePath
	track.FolderID = folderID(filepath.ToSlash(filepath.Dir(relativePath)))
	track.CoverTone = coverTone(track.ID)
	if info != nil {
		track.SizeBytes = info.Size()
		track.Writable = info.Mode().Perm()&0o222 != 0
		track.ModifiedAt = info.ModTime().Format("2006-01-02 15:04")
	}
	if statErr != nil {
		track.Health = domain.HealthParseError
		track.ParseError = statErr.Error()
		track.Revision = FileRevision(relativePath, info, nil)
		return track, statErr
	}

	snapshot, readErr := s.engine.Read(ctx, path)
	if readErr != nil {
		track.Health = domain.HealthParseError
		track.ParseError = readErr.Error()
		track.Revision = FileRevision(relativePath, info, nil)
		return track, readErr
	}
	applySnapshot(&track, snapshot)
	if snapshot.ArtworkCount > 0 {
		if artworkEngine, ok := s.engine.(tags.ArtworkEngine); ok {
			if data, artworkErr := artworkEngine.ReadArtwork(ctx, path, 0); artworkErr == nil {
				if asset, describeErr := artwork.Describe(data); describeErr == nil {
					track.ArtworkWidth = asset.Width
					track.ArtworkHeight = asset.Height
					track.ArtworkSizeBytes = int64(asset.Size)
				}
			}
		}
	}
	sidecarLyrics, sidecarInfo := readSidecar(path)
	if track.Lyrics == "" {
		track.Lyrics = sidecarLyrics
	}
	track.LyricsSidecar = sidecarInfo
	track.DurationSeconds = snapshotDurationSeconds(snapshot)
	track.Health = healthFor(track)
	track.Revision = FileRevision(relativePath, info, snapshot.Raw)
	return track, nil
}

func applySnapshot(track *domain.Track, snapshot tags.Snapshot) {
	track.Properties = snapshot.Properties
	track.ArtworkCount = snapshot.ArtworkCount
	track.Title = first(snapshot.Raw, "TITLE", "SUBTITLE", track.Title)
	track.Artists = values(snapshot.Raw, "ARTIST", "ARTISTS")
	if len(track.Artists) == 0 {
		_, track.Artists, _ = inferFromPath(track.RelativePath)
	}
	track.Album = first(snapshot.Raw, "ALBUM", track.Album)
	track.AlbumArtists = values(snapshot.Raw, "ALBUMARTIST", "ALBUM ARTIST")
	if len(track.AlbumArtists) == 0 {
		track.AlbumArtists = append([]string(nil), track.Artists...)
	}
	track.Genres = values(snapshot.Raw, "GENRE")
	track.Lyrics = first(snapshot.Raw, "LYRICS", "UNSYNCEDLYRICS", "UNSYNCED LYRICS", "")
	track.Comment = first(snapshot.Raw, "COMMENT", "DESCRIPTION", "")
	track.Composers = values(snapshot.Raw, "COMPOSER", "COMPOSERS")
	track.Conductor = first(snapshot.Raw, "CONDUCTOR", "")
	track.Lyricists = values(snapshot.Raw, "LYRICIST", "LYRICISTS")
	track.Copyright = first(snapshot.Raw, "COPYRIGHT", "")
	track.BPM = positiveInt(first(snapshot.Raw, "BPM", "TBPM", ""))
	track.ISRC = first(snapshot.Raw, "ISRC", "")
	track.MusicBrainzTrackID = first(snapshot.Raw, "MUSICBRAINZ_TRACKID", "MUSICBRAINZ_TRACK_ID", "")
	track.MusicBrainzReleaseID = first(snapshot.Raw, "MUSICBRAINZ_ALBUMID", "MUSICBRAINZ_RELEASEID", "MUSICBRAINZ_RELEASE_ID", "")
	track.MusicBrainzArtistIDs = values(snapshot.Raw, "MUSICBRAINZ_ARTISTID", "MUSICBRAINZ_ARTIST_ID")
	track.AcoustID = first(snapshot.Raw, "ACOUSTID_ID", "ACOUSTID", "")
	track.AcoustIDFingerprint = first(snapshot.Raw, "ACOUSTID_FINGERPRINT", "")
	track.TrackNumber, track.TrackTotal = indexValues(snapshot.Raw, "TRACKNUMBER", "TRACKTOTAL", "TOTALTRACKS")
	track.DiscNumber, track.DiscTotal = indexValues(snapshot.Raw, "DISCNUMBER", "DISCTOTAL", "TOTALDISCS")
	track.Year = yearValue(first(snapshot.Raw, "DATE", "YEAR", "RELEASEDATE", ""))
}

func fallbackTrack(relativePath string, format domain.TrackFormat) domain.Track {
	title, artists, album := inferFromPath(relativePath)
	return domain.Track{
		Format:               format,
		Title:                title,
		Artists:              artists,
		Album:                album,
		AlbumArtists:         append([]string(nil), artists...),
		Genres:               []string{},
		Composers:            []string{},
		Lyricists:            []string{},
		MusicBrainzArtistIDs: []string{},
		Health:               domain.HealthNeedsReview,
		Properties: domain.TrackProperties{
			Container: strings.ToUpper(string(format)),
			Codec:     strings.ToUpper(string(format)),
		},
	}
}

func inferFromPath(relativePath string) (string, []string, string) {
	directory := filepath.ToSlash(filepath.Dir(relativePath))
	base := strings.TrimSuffix(filepath.Base(relativePath), filepath.Ext(relativePath))
	if directory != "." {
		albumFolder := filepath.Base(directory)
		artist, album, found := strings.Cut(albumFolder, "-")
		if found && artist != "" && album != "" {
			title := strings.TrimPrefix(base, artist+"-")
			return title, []string{artist}, album
		}
	}
	if index := strings.LastIndex(base, "-"); index > 0 && index < len(base)-1 {
		return base[:index], []string{base[index+1:]}, ""
	}
	return base, []string{}, ""
}

func buildFolders(tracks []domain.Track) []domain.FolderNode {
	type folder struct {
		id    string
		name  string
		count int
	}
	folders := make(map[string]*folder)
	for _, track := range tracks {
		directory := filepath.ToSlash(filepath.Dir(track.RelativePath))
		name := "根目录单曲"
		if directory != "." {
			name = strings.ReplaceAll(directory, "/", " · ")
		}
		entry := folders[track.FolderID]
		if entry == nil {
			entry = &folder{id: track.FolderID, name: name}
			folders[track.FolderID] = entry
		}
		entry.count++
	}
	result := make([]domain.FolderNode, 0, len(folders))
	for _, entry := range folders {
		result = append(result, domain.FolderNode{ID: entry.id, Name: entry.name, Count: entry.count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID == "folder-root" {
			return true
		}
		if result[j].ID == "folder-root" {
			return false
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func healthFor(track domain.Track) domain.TrackHealth {
	if track.ParseError != "" {
		return domain.HealthParseError
	}
	if track.Title == "" || len(track.Artists) == 0 {
		return domain.HealthNeedsReview
	}
	if track.ArtworkCount == 0 {
		return domain.HealthMissingArtwork
	}
	if strings.TrimSpace(track.Lyrics) == "" {
		return domain.HealthMissingLyrics
	}
	return domain.HealthComplete
}

func values(raw map[string][]string, keys ...string) []string {
	for _, key := range keys {
		for rawKey, rawValues := range raw {
			if !strings.EqualFold(strings.TrimSpace(rawKey), key) {
				continue
			}
			result := make([]string, 0, len(rawValues))
			for _, value := range rawValues {
				if value = strings.TrimSpace(value); value != "" {
					result = append(result, value)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
	}
	return []string{}
}

func first(raw map[string][]string, keysAndFallback ...string) string {
	if len(keysAndFallback) == 0 {
		return ""
	}
	fallback := keysAndFallback[len(keysAndFallback)-1]
	if values := values(raw, keysAndFallback[:len(keysAndFallback)-1]...); len(values) > 0 {
		return values[0]
	}
	return fallback
}

func indexValues(raw map[string][]string, numberKey string, totalKeys ...string) (*int, *int) {
	var number, total *int
	if rawNumber := first(raw, numberKey, ""); rawNumber != "" {
		parts := strings.SplitN(rawNumber, "/", 2)
		number = positiveInt(parts[0])
		if len(parts) == 2 {
			total = positiveInt(parts[1])
		}
	}
	if total == nil {
		total = positiveInt(first(raw, append(totalKeys, "")...))
	}
	return number, total
}

func positiveInt(value string) *int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return nil
	}
	return &parsed
}

func yearValue(value string) *int {
	value = strings.TrimSpace(value)
	if len(value) < 4 {
		return nil
	}
	year, err := strconv.Atoi(value[:4])
	if err != nil || year < 1000 || year > 9999 {
		return nil
	}
	return &year
}

func snapshotDurationSeconds(snapshot tags.Snapshot) int64 {
	return snapshot.DurationSeconds
}

func readSidecar(path string) (string, *domain.SidecarInfo) {
	lyricsPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".lrc"
	info, err := os.Lstat(lyricsPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil
	}
	sidecar := &domain.SidecarInfo{Exists: true, SizeBytes: info.Size(), ModifiedAt: info.ModTime().Format("2006-01-02 15:04")}
	if info.Size() > domain.MaxSidecarLyricsBytes {
		return "", sidecar
	}
	content, err := os.ReadFile(lyricsPath)
	if err != nil {
		return "", sidecar
	}
	sidecar.Revision = domain.SidecarRevision(content)
	return string(content), sidecar
}

// FileRevision binds the indexed path, file identity and normalized raw tags.
// Writers recompute it immediately before editing to detect external changes.
func FileRevision(relativePath string, info fs.FileInfo, raw map[string][]string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(relativePath))
	if info != nil {
		_, _ = fmt.Fprintf(hash, "\x00%d\x00%d", info.Size(), info.ModTime().UnixNano())
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, _ = hash.Write([]byte("\x00" + key))
		for _, value := range raw[key] {
			_, _ = hash.Write([]byte("\x00" + value))
		}
	}
	return "rev-" + hex.EncodeToString(hash.Sum(nil)[:12])
}

func isSupportedAudio(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".flac", ".wav", ".wave":
		return true
	default:
		return false
	}
}

func formatFromPath(path string) domain.TrackFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return domain.FormatMP3
	case ".wav", ".wave":
		return domain.FormatWAV
	default:
		return domain.FormatFLAC
	}
}

func isIgnoredDirectory(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "@eadir", "$recycle.bin", "system volume information":
		return true
	default:
		return false
	}
}

func folderID(directory string) string {
	if directory == "." || directory == "" {
		return "folder-root"
	}
	return "folder-" + shortHash(directory)
}

func coverTone(seed string) domain.CoverTone {
	tone := []domain.CoverTone{
		domain.CoverVermilion,
		domain.CoverMoss,
		domain.CoverCobalt,
		domain.CoverSand,
		domain.CoverCharcoal,
		domain.CoverJade,
	}
	hash := sha256.Sum256([]byte(seed))
	return tone[int(hash[0])%len(tone)]
}

func shortHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:8])
}
