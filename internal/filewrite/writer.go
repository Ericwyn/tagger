package filewrite

import (
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

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/tags"
)

var (
	ErrRevisionConflict  = errors.New("revision conflict")
	ErrPathOutsideRoot   = errors.New("path outside library root")
	ErrInvalidPatch      = errors.New("invalid tag patch")
	ErrVerification      = errors.New("write verification failed")
	ErrUnsupportedFormat = errors.New("unwritable format")
)

type RevisionConflictError struct {
	Expected string
	Current  string
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("revision changed: expected %s, current %s", e.Expected, e.Current)
}

func (e *RevisionConflictError) Unwrap() error { return ErrRevisionConflict }

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

func (w *Writer) Write(ctx context.Context, ref library.FileRef, baseRevision string, patch domain.TagPatch, dryRun bool) (Result, error) {
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

	updates, diffs, err := compilePatch(before.Raw, patch)
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

func cloneRawTags(raw map[string][]string) map[string][]string {
	result := make(map[string][]string, len(raw))
	for key, values := range raw {
		result[key] = append([]string(nil), values...)
	}
	return result
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
