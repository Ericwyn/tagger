package filewrite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/tags"
)

var (
	ErrRevisionConflict   = errors.New("revision conflict")
	ErrPathOutsideRoot    = errors.New("path outside library root")
	ErrInvalidPatch       = errors.New("invalid tag patch")
	ErrVerification       = errors.New("write verification failed")
	ErrUnsupportedFormat  = errors.New("unwritable format")
	ErrArtworkUnavailable = errors.New("artwork operations unavailable")
	ErrArtworkNotFound    = errors.New("artwork not found")
	ErrArtworkIndex       = errors.New("invalid artwork index")
	ErrSidecarTooLarge    = errors.New("lyrics sidecar is too large")
	ErrSidecarConflict    = errors.New("lyrics sidecar revision conflict")
)

type RevisionConflictError struct {
	Expected string
	Current  string
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("revision changed: expected %s, current %s", e.Expected, e.Current)
}

func (e *RevisionConflictError) Unwrap() error { return ErrRevisionConflict }

type SidecarConflictError struct {
	Expected string
	Current  string
}

func (e *SidecarConflictError) Error() string {
	return fmt.Sprintf("lyrics sidecar changed: expected %s, current %s", e.Expected, e.Current)
}

func (e *SidecarConflictError) Unwrap() error { return ErrSidecarConflict }

type FieldDiff = domain.RevisionDiff

type Result struct {
	BaseRevision    string              `json:"baseRevision"`
	CurrentRevision string              `json:"currentRevision"`
	DryRun          bool                `json:"dryRun"`
	Changed         bool                `json:"changed"`
	Diff            []FieldDiff         `json:"diff"`
	Warnings        []string            `json:"warnings"`
	BeforeTags      map[string][]string `json:"-"`
	AfterTags       map[string][]string `json:"-"`
	Sidecar         *SidecarResult      `json:"sidecar,omitempty"`
}

type ArtworkResult struct {
	BaseRevision    string              `json:"baseRevision"`
	CurrentRevision string              `json:"currentRevision"`
	DryRun          bool                `json:"dryRun"`
	Changed         bool                `json:"changed"`
	Diff            []FieldDiff         `json:"diff"`
	Warnings        []string            `json:"warnings"`
	Before          *artwork.Asset      `json:"before,omitempty"`
	After           *artwork.Asset      `json:"after,omitempty"`
	BeforeTags      map[string][]string `json:"-"`
	AfterTags       map[string][]string `json:"-"`
}

type SidecarSnapshot struct {
	Info    *domain.SidecarInfo
	Content string
}

type SidecarResult struct {
	BaseRevision           string              `json:"baseRevision"`
	CurrentRevision        string              `json:"currentRevision"`
	BaseSidecarRevision    string              `json:"baseSidecarRevision"`
	CurrentSidecarRevision string              `json:"currentSidecarRevision,omitempty"`
	DryRun                 bool                `json:"dryRun"`
	Changed                bool                `json:"changed"`
	Warnings               []string            `json:"warnings"`
	Before                 *domain.SidecarInfo `json:"before,omitempty"`
	After                  *domain.SidecarInfo `json:"after,omitempty"`
	BeforeContent          string              `json:"-"`
	AfterContent           string              `json:"-"`
}

type Writer struct {
	root   string
	engine tags.Engine
	locks  sync.Map
}

func New(root string, engine tags.Engine) (*Writer, error) {
	if engine == nil {
		return nil, fmt.Errorf("tag engine is required")
	}
	root, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve library root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat library root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("library root is not a directory")
	}
	return &Writer{root: root, engine: engine}, nil
}

// OpenRead opens an indexed audio file after applying the same library-root
// and symlink checks used by write operations. The caller owns the returned
// file and must close it after consuming the stream.
func (w *Writer) OpenRead(ref library.FileRef) (*os.File, error) {
	path, err := w.containedPath(ref)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// ReadRawTags reads the lossless TagLib property map for an indexed file
// without exposing the absolute path to callers. The returned map is owned by
// the caller and can be safely modified.
func (w *Writer) ReadRawTags(ctx context.Context, ref library.FileRef) (map[string][]string, error) {
	path, err := w.containedPath(ref)
	if err != nil {
		return nil, err
	}
	snapshot, err := w.engine.Read(ctx, path)
	if err != nil {
		return nil, err
	}
	return cloneRawTags(snapshot.Raw), nil
}

// ReadSidecar reads the optional same-basename LRC file after applying the
// audio file's library-root and symlink checks.
func (w *Writer) ReadSidecar(ctx context.Context, ref library.FileRef) (SidecarSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SidecarSnapshot{}, err
	}
	path, err := w.containedSidecarPath(ref)
	if err != nil {
		return SidecarSnapshot{}, err
	}
	return readSidecarFile(path)
}

// WriteSidecar atomically creates, replaces, or removes the LRC sidecar. A
// nil content pointer means delete; an empty non-nil string creates an empty
// sidecar. Both the audio revision and sidecar content revision are guarded.
func (w *Writer) WriteSidecar(ctx context.Context, ref library.FileRef, baseRevision, baseSidecarRevision string, content *string, dryRun bool) (SidecarResult, error) {
	if ref.Format != domain.FormatMP3 && ref.Format != domain.FormatFLAC && ref.Format != domain.FormatWAV {
		return SidecarResult{}, ErrUnsupportedFormat
	}
	path, err := w.containedPath(ref)
	if err != nil {
		return SidecarResult{}, err
	}
	sidecarPath, err := w.containedSidecarPath(ref)
	if err != nil {
		return SidecarResult{}, err
	}
	lockValue, _ := w.locks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return SidecarResult{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return SidecarResult{}, fmt.Errorf("stat source file: %w", err)
	}
	snapshot, err := w.engine.Read(ctx, path)
	if err != nil {
		return SidecarResult{}, fmt.Errorf("read source before sidecar write: %w", err)
	}
	currentRevision := scanner.FileRevision(ref.RelativePath, info, snapshot.Raw)
	if baseRevision == "" || baseRevision != currentRevision {
		return SidecarResult{}, &RevisionConflictError{Expected: baseRevision, Current: currentRevision}
	}
	before, err := readSidecarFile(sidecarPath)
	if err != nil {
		return SidecarResult{}, err
	}
	currentSidecarRevision := ""
	if before.Info != nil {
		currentSidecarRevision = before.Info.Revision
	}
	if baseSidecarRevision != currentSidecarRevision {
		return SidecarResult{}, &SidecarConflictError{Expected: baseSidecarRevision, Current: currentSidecarRevision}
	}
	if content != nil && len([]byte(*content)) > domain.MaxSidecarLyricsBytes {
		return SidecarResult{}, ErrSidecarTooLarge
	}
	changed := content != nil
	if content != nil {
		changed = !before.Exists() || before.Content != *content
	} else {
		changed = before.Exists()
	}
	result := SidecarResult{
		BaseRevision: baseRevision, CurrentRevision: currentRevision,
		BaseSidecarRevision: baseSidecarRevision, CurrentSidecarRevision: currentSidecarRevision,
		DryRun: dryRun, Changed: changed, Warnings: []string{}, Before: cloneSidecarInfo(before.Info), BeforeContent: before.Content,
	}
	if !changed || dryRun {
		if content != nil {
			result.CurrentSidecarRevision = domain.SidecarRevision([]byte(*content))
			result.After = &domain.SidecarInfo{Exists: true, Revision: result.CurrentSidecarRevision, SizeBytes: int64(len([]byte(*content)))}
			result.AfterContent = *content
		}
		return result, nil
	}
	if content == nil {
		if err := os.Remove(sidecarPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return SidecarResult{}, fmt.Errorf("remove lyrics sidecar: %w", err)
		}
		if err := syncDirectory(filepath.Dir(sidecarPath)); err != nil {
			return SidecarResult{}, fmt.Errorf("sync sidecar directory: %w", err)
		}
	} else {
		var sidecarInfo fs.FileInfo
		if before.Info != nil {
			sidecarInfo, _ = os.Stat(sidecarPath)
		}
		if err := writeSidecarAtomic(sidecarPath, []byte(*content), sidecarInfo); err != nil {
			return SidecarResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return SidecarResult{}, err
	}
	after, err := readSidecarFile(sidecarPath)
	if err != nil {
		return SidecarResult{}, err
	}
	if content == nil {
		if after.Exists() {
			return SidecarResult{}, fmt.Errorf("%w: sidecar still exists after delete", ErrVerification)
		}
	} else if after.Content != *content {
		return SidecarResult{}, fmt.Errorf("%w: sidecar content differs after write", ErrVerification)
	}
	if after.Info != nil {
		result.CurrentSidecarRevision = after.Info.Revision
	}
	result.After = cloneSidecarInfo(after.Info)
	result.AfterContent = after.Content
	return result, nil
}

func (w *Writer) Write(ctx context.Context, ref library.FileRef, baseRevision string, patch domain.TagPatch, dryRun bool) (Result, error) {
	return w.mutate(ctx, ref, baseRevision, dryRun, func(raw map[string][]string) (map[string][]string, []FieldDiff, error) {
		return compilePatch(raw, patch)
	})
}

func (w *Writer) Restore(ctx context.Context, ref library.FileRef, baseRevision string, target map[string][]string, dryRun bool) (Result, error) {
	if target == nil {
		return Result{}, fmt.Errorf("%w: restore target is missing", ErrInvalidPatch)
	}
	result, err := w.mutate(ctx, ref, baseRevision, dryRun, func(raw map[string][]string) (map[string][]string, []FieldDiff, error) {
		return compileRestore(raw, target)
	})
	if err != nil {
		return Result{}, err
	}
	result.Warnings = append(result.Warnings, "恢复仅覆盖 Tagger 管理的标准字段；未知或格式私有标签保持当前值")
	return result, nil
}

func (w *Writer) ReadArtwork(ctx context.Context, ref library.FileRef, index int) (artwork.Asset, error) {
	if index < 0 || index > 31 {
		return artwork.Asset{}, ErrArtworkIndex
	}
	engine, ok := w.engine.(tags.ArtworkEngine)
	if !ok {
		return artwork.Asset{}, ErrArtworkUnavailable
	}
	path, err := w.containedPath(ref)
	if err != nil {
		return artwork.Asset{}, err
	}
	lockValue, _ := w.locks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	data, err := engine.ReadArtwork(ctx, path, index)
	if err != nil {
		return artwork.Asset{}, err
	}
	if len(data) == 0 {
		return artwork.Asset{}, ErrArtworkNotFound
	}
	asset, err := artwork.Describe(data)
	if err != nil {
		return artwork.Asset{}, fmt.Errorf("describe embedded artwork: %w", err)
	}
	return asset, nil
}

func (w *Writer) WriteArtwork(ctx context.Context, ref library.FileRef, baseRevision string, index int, target *artwork.Asset, dryRun bool) (ArtworkResult, error) {
	if ref.Format != domain.FormatMP3 && ref.Format != domain.FormatFLAC && ref.Format != domain.FormatWAV {
		return ArtworkResult{}, ErrUnsupportedFormat
	}
	if index < 0 || index > 31 {
		return ArtworkResult{}, ErrArtworkIndex
	}
	engine, ok := w.engine.(tags.ArtworkEngine)
	if !ok {
		return ArtworkResult{}, ErrArtworkUnavailable
	}
	path, err := w.containedPath(ref)
	if err != nil {
		return ArtworkResult{}, err
	}
	lockValue, _ := w.locks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return ArtworkResult{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return ArtworkResult{}, fmt.Errorf("stat source file: %w", err)
	}
	beforeSnapshot, err := w.engine.Read(ctx, path)
	if err != nil {
		return ArtworkResult{}, fmt.Errorf("read source before artwork write: %w", err)
	}
	currentRevision := scanner.FileRevision(ref.RelativePath, info, beforeSnapshot.Raw)
	if baseRevision == "" || baseRevision != currentRevision {
		return ArtworkResult{}, &RevisionConflictError{Expected: baseRevision, Current: currentRevision}
	}
	beforeData, err := engine.ReadArtwork(ctx, path, index)
	if err != nil {
		return ArtworkResult{}, fmt.Errorf("read current artwork: %w", err)
	}
	var beforeAsset *artwork.Asset
	if len(beforeData) > 0 {
		described, err := artwork.Describe(beforeData)
		if err != nil {
			return ArtworkResult{}, fmt.Errorf("describe current artwork: %w", err)
		}
		beforeAsset = &described
	}
	changed := (beforeAsset == nil) != (target == nil)
	if beforeAsset != nil && target != nil {
		changed = beforeAsset.Hash != target.Hash
	}
	operation := domain.OperationSet
	if target == nil {
		operation = domain.OperationDelete
	}
	result := ArtworkResult{
		BaseRevision: baseRevision, CurrentRevision: currentRevision, DryRun: dryRun, Changed: changed,
		Diff: []FieldDiff{}, Warnings: []string{}, Before: beforeAsset, After: cloneArtworkAsset(target),
		BeforeTags: cloneRawTags(beforeSnapshot.Raw), AfterTags: cloneRawTags(beforeSnapshot.Raw),
	}
	if changed {
		result.Diff = append(result.Diff, FieldDiff{Field: "artwork", Operation: operation, Before: beforeAsset, After: target})
	}
	if dryRun || !changed {
		return result, nil
	}

	tempPath, err := copyToTemporary(path, info)
	if err != nil {
		return ArtworkResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if ownershipErr := preserveOwnership(tempPath, info); ownershipErr != nil {
		result.Warnings = append(result.Warnings, "无法保留原文件所有者信息："+ownershipErr.Error())
	}
	var targetData []byte
	var targetMIME string
	if target != nil {
		targetData, targetMIME = target.Data, target.MIME
	}
	if err := engine.WriteArtwork(ctx, tempPath, index, targetData, targetMIME); err != nil {
		return ArtworkResult{}, fmt.Errorf("write temporary artwork: %w", err)
	}
	afterSnapshot, err := w.engine.Read(ctx, tempPath)
	if err != nil {
		return ArtworkResult{}, fmt.Errorf("verify artwork properties: %w", err)
	}
	afterData, err := engine.ReadArtwork(ctx, tempPath, index)
	if err != nil {
		return ArtworkResult{}, fmt.Errorf("verify artwork bytes: %w", err)
	}
	if target != nil && !bytes.Equal(afterData, target.Data) {
		return ArtworkResult{}, fmt.Errorf("%w: embedded artwork bytes differ after write", ErrVerification)
	}
	if target == nil && afterSnapshot.ArtworkCount != max(0, beforeSnapshot.ArtworkCount-1) {
		return ArtworkResult{}, fmt.Errorf("%w: artwork count is %d, want %d", ErrVerification, afterSnapshot.ArtworkCount, max(0, beforeSnapshot.ArtworkCount-1))
	}
	var afterAsset *artwork.Asset
	if len(afterData) > 0 {
		described, err := artwork.Describe(afterData)
		if err != nil {
			return ArtworkResult{}, fmt.Errorf("describe written artwork: %w", err)
		}
		afterAsset = &described
	}
	if err := syncFile(tempPath); err != nil {
		return ArtworkResult{}, fmt.Errorf("sync temporary copy: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return ArtworkResult{}, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return ArtworkResult{}, fmt.Errorf("atomically replace source: %w", err)
	}
	committed = true
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		result.Warnings = append(result.Warnings, "目录同步失败："+err.Error())
	}
	finalInfo, err := os.Stat(path)
	if err != nil {
		return ArtworkResult{}, fmt.Errorf("stat written file: %w", err)
	}
	result.CurrentRevision = scanner.FileRevision(ref.RelativePath, finalInfo, afterSnapshot.Raw)
	result.After = afterAsset
	result.AfterTags = cloneRawTags(afterSnapshot.Raw)
	return result, nil
}

func cloneArtworkAsset(asset *artwork.Asset) *artwork.Asset {
	if asset == nil {
		return nil
	}
	clone := *asset
	clone.Data = append([]byte(nil), asset.Data...)
	return &clone
}

type updateCompiler func(raw map[string][]string) (map[string][]string, []FieldDiff, error)

func (w *Writer) mutate(ctx context.Context, ref library.FileRef, baseRevision string, dryRun bool, compile updateCompiler) (Result, error) {
	if ref.Format != domain.FormatMP3 && ref.Format != domain.FormatFLAC && ref.Format != domain.FormatWAV {
		return Result{}, ErrUnsupportedFormat
	}
	path, err := w.containedPath(ref)
	if err != nil {
		return Result{}, err
	}
	lockValue, _ := w.locks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, fmt.Errorf("stat source file: %w", err)
	}
	before, err := w.engine.Read(ctx, path)
	if err != nil {
		return Result{}, fmt.Errorf("read source before write: %w", err)
	}
	currentRevision := scanner.FileRevision(ref.RelativePath, info, before.Raw)
	if baseRevision == "" || baseRevision != currentRevision {
		return Result{}, &RevisionConflictError{Expected: baseRevision, Current: currentRevision}
	}

	updates, diffs, err := compile(before.Raw)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		BaseRevision:    baseRevision,
		CurrentRevision: currentRevision,
		DryRun:          dryRun,
		Changed:         len(diffs) > 0,
		Diff:            diffs,
		Warnings:        []string{},
		BeforeTags:      cloneRawTags(before.Raw),
	}
	if dryRun || len(diffs) == 0 {
		result.AfterTags = cloneRawTags(before.Raw)
		return result, nil
	}

	tempPath, err := copyToTemporary(path, info)
	if err != nil {
		return Result{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	if ownershipErr := preserveOwnership(tempPath, info); ownershipErr != nil {
		result.Warnings = append(result.Warnings, "无法保留原文件所有者信息："+ownershipErr.Error())
	}
	if err := w.engine.Write(ctx, tempPath, updates); err != nil {
		return Result{}, fmt.Errorf("write temporary copy: %w", err)
	}
	after, err := w.engine.Read(ctx, tempPath)
	if err != nil {
		return Result{}, fmt.Errorf("verify temporary copy: %w", err)
	}
	if err := verifyUpdates(after.Raw, updates); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrVerification, err)
	}
	if err := syncFile(tempPath); err != nil {
		return Result{}, fmt.Errorf("sync temporary copy: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return Result{}, fmt.Errorf("atomically replace source: %w", err)
	}
	committed = true
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		result.Warnings = append(result.Warnings, "目录同步失败："+err.Error())
	}

	finalInfo, err := os.Stat(path)
	if err != nil {
		return Result{}, fmt.Errorf("stat written file: %w", err)
	}
	result.CurrentRevision = scanner.FileRevision(ref.RelativePath, finalInfo, after.Raw)
	result.AfterTags = cloneRawTags(after.Raw)
	return result, nil
}

type restorableField struct {
	field string
	key   string
	multi bool
}

var restorableFields = []restorableField{
	{field: "title", key: "TITLE"},
	{field: "artists", key: "ARTIST", multi: true},
	{field: "album", key: "ALBUM"},
	{field: "albumArtists", key: "ALBUMARTIST", multi: true},
	{field: "trackNumber", key: "TRACKNUMBER"},
	{field: "trackTotal", key: "TRACKTOTAL"},
	{field: "discNumber", key: "DISCNUMBER"},
	{field: "discTotal", key: "DISCTOTAL"},
	{field: "year", key: "DATE"},
	{field: "genres", key: "GENRE", multi: true},
	{field: "lyrics", key: "LYRICS"},
	{field: "comment", key: "COMMENT"},
	{field: "composers", key: "COMPOSER", multi: true},
	{field: "conductor", key: "CONDUCTOR"},
	{field: "lyricists", key: "LYRICIST", multi: true},
	{field: "copyright", key: "COPYRIGHT"},
	{field: "bpm", key: "BPM"},
	{field: "isrc", key: "ISRC"},
	{field: "musicbrainzTrackId", key: "MUSICBRAINZ_TRACKID"},
	{field: "musicbrainzReleaseId", key: "MUSICBRAINZ_ALBUMID"},
	{field: "musicbrainzArtistIds", key: "MUSICBRAINZ_ARTISTID", multi: true},
	{field: "acoustidId", key: "ACOUSTID_ID"},
	{field: "acoustidFingerprint", key: "ACOUSTID_FINGERPRINT"},
}

func compileRestore(current, target map[string][]string) (map[string][]string, []FieldDiff, error) {
	updates := make(map[string][]string)
	diffs := make([]FieldDiff, 0, len(restorableFields))
	for _, field := range restorableFields {
		before := rawValues(current, field.key)
		after := rawValues(target, field.key)
		if slices.Equal(before, after) {
			continue
		}
		operation := domain.OperationSet
		if len(after) == 0 {
			operation = domain.OperationDelete
		}
		updates[field.key] = append([]string(nil), after...)
		diffs = append(diffs, FieldDiff{
			Field: field.field, Operation: operation,
			Before: restoreDiffValue(before, field.multi), After: restoreDiffValue(after, field.multi),
		})
	}
	return updates, diffs, nil
}

func restoreDiffValue(values []string, multi bool) any {
	if multi {
		return append([]string(nil), values...)
	}
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func cloneRawTags(raw map[string][]string) map[string][]string {
	result := make(map[string][]string, len(raw))
	for key, values := range raw {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func (s SidecarSnapshot) Exists() bool {
	return s.Info != nil && s.Info.Exists
}

func cloneSidecarInfo(info *domain.SidecarInfo) *domain.SidecarInfo {
	if info == nil {
		return nil
	}
	clone := *info
	return &clone
}

func readSidecarFile(path string) (SidecarSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return SidecarSnapshot{}, nil
	}
	if err != nil {
		return SidecarSnapshot{}, fmt.Errorf("stat lyrics sidecar: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return SidecarSnapshot{}, fmt.Errorf("%w: lyrics sidecar is a symbolic link", ErrPathOutsideRoot)
	}
	if !info.Mode().IsRegular() {
		return SidecarSnapshot{}, fmt.Errorf("%w: lyrics sidecar is not a regular file", ErrPathOutsideRoot)
	}
	result := SidecarSnapshot{Info: &domain.SidecarInfo{Exists: true, SizeBytes: info.Size(), ModifiedAt: info.ModTime().Format("2006-01-02 15:04")}}
	if info.Size() > domain.MaxSidecarLyricsBytes {
		return result, ErrSidecarTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SidecarSnapshot{}, fmt.Errorf("read lyrics sidecar: %w", err)
	}
	result.Content = string(data)
	result.Info.Revision = domain.SidecarRevision(data)
	return result, nil
}

func (w *Writer) containedSidecarPath(ref library.FileRef) (string, error) {
	audioPath, err := w.containedPath(ref)
	if err != nil {
		return "", err
	}
	path := strings.TrimSuffix(audioPath, filepath.Ext(audioPath)) + ".lrc"
	info, statErr := os.Lstat(path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", ErrPathOutsideRoot
	}
	return path, nil
}

func writeSidecarAtomic(path string, data []byte, sourceInfo fs.FileInfo) error {
	directory := filepath.Dir(path)
	base := filepath.Base(path)
	temporary, err := os.CreateTemp(directory, "."+base+".tagger-*")
	if err != nil {
		return fmt.Errorf("create temporary sidecar: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	mode := fs.FileMode(0o644)
	if sourceInfo != nil {
		mode = sourceInfo.Mode().Perm()
	}
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set sidecar mode: %w", err)
	}
	if sourceInfo != nil {
		_ = preserveOwnership(temporaryPath, sourceInfo)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary sidecar: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary sidecar: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary sidecar: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("atomically replace sidecar: %w", err)
	}
	committed = true
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync sidecar directory: %w", err)
	}
	return nil
}

func (w *Writer) containedPath(ref library.FileRef) (string, error) {
	if ref.RelativePath == "" || filepath.IsAbs(ref.RelativePath) || strings.ContainsRune(ref.RelativePath, '\x00') {
		return "", ErrPathOutsideRoot
	}
	cleanRelative := filepath.Clean(filepath.FromSlash(ref.RelativePath))
	if cleanRelative == ".." || strings.HasPrefix(cleanRelative, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideRoot
	}
	expected := filepath.Join(w.root, cleanRelative)
	expectedAbs, err := filepath.Abs(expected)
	if err != nil {
		return "", ErrPathOutsideRoot
	}
	refAbs, err := filepath.Abs(ref.AbsolutePath)
	if err != nil || refAbs != expectedAbs {
		return "", ErrPathOutsideRoot
	}
	info, err := os.Lstat(expectedAbs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", ErrPathOutsideRoot
	}
	rootResolved, err := filepath.EvalSymlinks(w.root)
	if err != nil {
		return "", err
	}
	pathResolved, err := filepath.EvalSymlinks(expectedAbs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootResolved, pathResolved)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideRoot
	}
	return expectedAbs, nil
}

func compilePatch(raw map[string][]string, patch domain.TagPatch) (map[string][]string, []FieldDiff, error) {
	updates := make(map[string][]string)
	diffs := make([]FieldDiff, 0, 11)

	if err := applyString(updates, &diffs, raw, "title", "TITLE", patch.Title); err != nil {
		return nil, nil, err
	}
	if err := applyStrings(updates, &diffs, raw, "artists", "ARTIST", patch.Artists); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "album", "ALBUM", patch.Album); err != nil {
		return nil, nil, err
	}
	if err := applyStrings(updates, &diffs, raw, "albumArtists", "ALBUMARTIST", patch.AlbumArtists); err != nil {
		return nil, nil, err
	}
	if err := applyInt(updates, &diffs, raw, "trackNumber", "TRACKNUMBER", patch.TrackNumber, 1, 9999); err != nil {
		return nil, nil, err
	}
	if err := applyInt(updates, &diffs, raw, "trackTotal", "TRACKTOTAL", patch.TrackTotal, 1, 9999); err != nil {
		return nil, nil, err
	}
	if err := applyInt(updates, &diffs, raw, "discNumber", "DISCNUMBER", patch.DiscNumber, 1, 999); err != nil {
		return nil, nil, err
	}
	if err := applyInt(updates, &diffs, raw, "discTotal", "DISCTOTAL", patch.DiscTotal, 1, 999); err != nil {
		return nil, nil, err
	}
	if err := applyInt(updates, &diffs, raw, "year", "DATE", patch.Year, 1000, 9999); err != nil {
		return nil, nil, err
	}
	if err := applyStrings(updates, &diffs, raw, "genres", "GENRE", patch.Genres); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "lyrics", "LYRICS", patch.Lyrics); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "comment", "COMMENT", patch.Comment); err != nil {
		return nil, nil, err
	}
	if err := applyStrings(updates, &diffs, raw, "composers", "COMPOSER", patch.Composers); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "conductor", "CONDUCTOR", patch.Conductor); err != nil {
		return nil, nil, err
	}
	if err := applyStrings(updates, &diffs, raw, "lyricists", "LYRICIST", patch.Lyricists); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "copyright", "COPYRIGHT", patch.Copyright); err != nil {
		return nil, nil, err
	}
	if err := applyInt(updates, &diffs, raw, "bpm", "BPM", patch.BPM, 1, 1000); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "isrc", "ISRC", patch.ISRC); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "musicbrainzTrackId", "MUSICBRAINZ_TRACKID", patch.MusicBrainzTrackID); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "musicbrainzReleaseId", "MUSICBRAINZ_ALBUMID", patch.MusicBrainzReleaseID); err != nil {
		return nil, nil, err
	}
	if err := applyStrings(updates, &diffs, raw, "musicbrainzArtistIds", "MUSICBRAINZ_ARTISTID", patch.MusicBrainzArtistIDs); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "acoustidId", "ACOUSTID_ID", patch.AcoustID); err != nil {
		return nil, nil, err
	}
	if err := applyString(updates, &diffs, raw, "acoustidFingerprint", "ACOUSTID_FINGERPRINT", patch.AcoustIDFingerprint); err != nil {
		return nil, nil, err
	}
	return updates, diffs, nil
}

func applyString(updates map[string][]string, diffs *[]FieldDiff, raw map[string][]string, field, key string, patch *domain.StringFieldPatch) error {
	if patch == nil || patch.Op == domain.OperationKeep {
		return nil
	}
	beforeValues := rawValues(raw, key)
	before := ""
	if len(beforeValues) > 0 {
		before = beforeValues[0]
	}
	var after string
	switch patch.Op {
	case domain.OperationSet:
		after = strings.TrimSpace(patch.Value)
	case domain.OperationDelete:
		after = ""
	default:
		return fmt.Errorf("%w: %s does not support %q", ErrInvalidPatch, field, patch.Op)
	}
	if before == after {
		return nil
	}
	if after == "" {
		updates[key] = []string{}
	} else {
		updates[key] = []string{after}
	}
	*diffs = append(*diffs, FieldDiff{Field: field, Operation: patch.Op, Before: before, After: after})
	return nil
}

func applyStrings(updates map[string][]string, diffs *[]FieldDiff, raw map[string][]string, field, key string, patch *domain.StringsFieldPatch) error {
	if patch == nil || patch.Op == domain.OperationKeep {
		return nil
	}
	before := rawValues(raw, key)
	var after []string
	switch patch.Op {
	case domain.OperationSet:
		after = cleanValues(patch.Value)
	case domain.OperationDelete:
		after = []string{}
	case domain.OperationMerge:
		after = mergeValues(before, cleanValues(patch.Value))
	default:
		return fmt.Errorf("%w: invalid operation %q for %s", ErrInvalidPatch, patch.Op, field)
	}
	if slices.Equal(before, after) {
		return nil
	}
	updates[key] = after
	*diffs = append(*diffs, FieldDiff{Field: field, Operation: patch.Op, Before: before, After: after})
	return nil
}

func applyInt(updates map[string][]string, diffs *[]FieldDiff, raw map[string][]string, field, key string, patch *domain.IntFieldPatch, minimum, maximum int) error {
	if patch == nil || patch.Op == domain.OperationKeep {
		return nil
	}
	before := ""
	if values := rawValues(raw, key); len(values) > 0 {
		before = values[0]
	}
	after := ""
	switch patch.Op {
	case domain.OperationSet:
		if patch.Value < minimum || patch.Value > maximum {
			return fmt.Errorf("%w: %s must be between %d and %d", ErrInvalidPatch, field, minimum, maximum)
		}
		after = strconv.Itoa(patch.Value)
	case domain.OperationDelete:
	default:
		return fmt.Errorf("%w: invalid operation %q for %s", ErrInvalidPatch, patch.Op, field)
	}
	if before == after {
		return nil
	}
	if after == "" {
		updates[key] = []string{}
	} else {
		updates[key] = []string{after}
	}
	*diffs = append(*diffs, FieldDiff{Field: field, Operation: patch.Op, Before: before, After: after})
	return nil
}

func verifyUpdates(raw, expected map[string][]string) error {
	for key, values := range expected {
		actual := rawValues(raw, key)
		if !slices.Equal(cleanValues(actual), cleanValues(values)) {
			return fmt.Errorf("field %s: got %q, want %q", key, actual, values)
		}
	}
	return nil
}

func rawValues(raw map[string][]string, key string) []string {
	for rawKey, values := range raw {
		if strings.EqualFold(strings.TrimSpace(rawKey), key) {
			return cleanValues(values)
		}
	}
	return []string{}
}

func cleanValues(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func mergeValues(current, additions []string) []string {
	return cleanValues(append(slices.Clone(current), additions...))
}

func copyToTemporary(sourcePath string, sourceInfo fs.FileInfo) (string, error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("open source file: %w", err)
	}
	defer source.Close()

	extension := filepath.Ext(sourcePath)
	base := strings.TrimSuffix(filepath.Base(sourcePath), extension)
	temporary, err := os.CreateTemp(filepath.Dir(sourcePath), "."+base+".tagger-*"+extension)
	if err != nil {
		return "", fmt.Errorf("create temporary copy: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		_ = temporary.Close()
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(sourceInfo.Mode().Perm()); err != nil {
		return "", fmt.Errorf("copy source mode: %w", err)
	}
	if _, err := io.Copy(temporary, source); err != nil {
		return "", fmt.Errorf("copy source content: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync source copy: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close source copy: %w", err)
	}
	remove = false
	return temporaryPath, nil
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
