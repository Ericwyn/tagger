// Package organizer plans and executes safe, in-library file moves.
package organizer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ericwyn/tagger/internal/domain"
)

var (
	ErrPathOutsideRoot = errors.New("organize path is outside library root")
	ErrTargetExists    = errors.New("organize target already exists")
	ErrSourceMissing   = errors.New("organize source is missing")
)

type Plan struct {
	TrackID       string
	Source        string
	Target        string
	SourceAbs     string
	TargetAbs     string
	PrimaryArtist string
	Album         string
	SidecarSource string
	SidecarTarget string
	SidecarExists bool
	State         domain.OrganizeItemState
	Warnings      []string
}

type Planner struct {
	root string
}

func NewPlanner(root string) (*Planner, error) {
	root, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve library root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("stat library root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("library root is not a regular directory")
	}
	return &Planner{root: root}, nil
}

func (p *Planner) Root() string { return p.root }

func (p *Planner) Plan(ctx context.Context, track domain.Track, moveLyricsSidecar bool) (Plan, error) {
	return p.PlanWithMode(ctx, track, domain.DefaultOrganizeMode, moveLyricsSidecar)
}

func (p *Planner) PlanWithMode(ctx context.Context, track domain.Track, mode domain.OrganizeMode, moveLyricsSidecar bool) (Plan, error) {
	return p.PlanWithBasePath(ctx, track, "", mode, moveLyricsSidecar)
}

func (p *Planner) PlanWithBasePath(ctx context.Context, track domain.Track, basePath string, mode domain.OrganizeMode, moveLyricsSidecar bool) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	mode = domain.NormalizeOrganizeMode(mode)
	if !mode.Valid() {
		return Plan{}, fmt.Errorf("unsupported organize mode: %q", mode)
	}
	basePath, err := NormalizeBasePath(basePath)
	if err != nil {
		return Plan{}, err
	}
	sourceRel, err := containedRelative(track.RelativePath)
	if err != nil {
		return Plan{}, err
	}
	if basePath != "" && !relativeWithinBase(sourceRel, basePath) {
		return Plan{}, fmt.Errorf("source path is outside organize base path")
	}
	if strings.TrimSpace(track.FileName) == "" || filepath.Base(filepath.FromSlash(track.FileName)) != track.FileName {
		return Plan{}, fmt.Errorf("invalid track file name")
	}
	artist := primaryArtist(track.Artists)
	if artist == "" {
		artist = "未知歌手"
	}
	album := sanitizeSegment(track.Album, "未知专辑")
	artist = sanitizeSegment(artist, "未知歌手")
	fileName := filepath.Base(filepath.FromSlash(track.FileName))
	pathParts := []string{artist}
	if mode == domain.OrganizeModeArtistAlbum {
		pathParts = append(pathParts, album)
	}
	pathParts = append(pathParts, fileName)
	if basePath != "" {
		pathParts = append([]string{basePath}, pathParts...)
	}
	targetRel := filepath.ToSlash(filepath.Join(pathParts...))
	if _, err := containedRelative(targetRel); err != nil {
		return Plan{}, err
	}
	sourceAbs, err := p.containedPath(sourceRel)
	if err != nil {
		return Plan{}, err
	}
	targetAbs, err := p.containedPath(targetRel)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		TrackID:       track.ID,
		Source:        sourceRel,
		Target:        targetRel,
		SourceAbs:     sourceAbs,
		TargetAbs:     targetAbs,
		PrimaryArtist: artist,
		Album:         album,
		State:         domain.OrganizeReady,
		Warnings:      []string{},
	}
	if sourceRel == targetRel {
		plan.State = domain.OrganizeNoop
		return plan, nil
	}
	if err := p.checkSource(sourceAbs); err != nil {
		plan.State = domain.OrganizeInvalid
		plan.Warnings = append(plan.Warnings, err.Error())
		return plan, nil
	}
	if err := p.checkAncestors(targetAbs); err != nil {
		plan.State = domain.OrganizeInvalid
		plan.Warnings = append(plan.Warnings, err.Error())
		return plan, nil
	}
	if info, err := os.Lstat(targetAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			plan.State = domain.OrganizeConflict
			plan.Warnings = append(plan.Warnings, "目标路径不是普通文件")
		} else {
			plan.State = domain.OrganizeConflict
			plan.Warnings = append(plan.Warnings, ErrTargetExists.Error())
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		plan.State = domain.OrganizeInvalid
		plan.Warnings = append(plan.Warnings, "检查目标路径失败："+err.Error())
	}
	if moveLyricsSidecar {
		sidecarSource := strings.TrimSuffix(sourceAbs, filepath.Ext(sourceAbs)) + ".lrc"
		sidecarTarget := strings.TrimSuffix(targetAbs, filepath.Ext(targetAbs)) + ".lrc"
		if info, err := os.Lstat(sidecarSource); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				plan.State = domain.OrganizeInvalid
				plan.Warnings = append(plan.Warnings, "歌词 sidecar 不是普通文件")
			} else {
				plan.SidecarExists = true
				plan.SidecarSource = filepath.ToSlash(filepath.Join(filepath.Dir(sourceRel), filepath.Base(sidecarSource)))
				plan.SidecarTarget = filepath.ToSlash(filepath.Join(filepath.Dir(targetRel), filepath.Base(sidecarTarget)))
				if _, targetErr := os.Lstat(sidecarTarget); targetErr == nil {
					plan.State = domain.OrganizeConflict
					plan.Warnings = append(plan.Warnings, "目标歌词 sidecar 已存在")
				} else if !errors.Is(targetErr, fs.ErrNotExist) {
					plan.State = domain.OrganizeInvalid
					plan.Warnings = append(plan.Warnings, "检查目标歌词 sidecar 失败："+targetErr.Error())
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			plan.State = domain.OrganizeInvalid
			plan.Warnings = append(plan.Warnings, "检查歌词 sidecar 失败："+err.Error())
		}
	}
	return plan, nil
}

func (p *Planner) PlanMany(ctx context.Context, tracks []domain.Track, moveLyricsSidecar bool) ([]Plan, error) {
	return p.PlanManyWithMode(ctx, tracks, domain.DefaultOrganizeMode, moveLyricsSidecar)
}

func (p *Planner) PlanManyWithMode(ctx context.Context, tracks []domain.Track, mode domain.OrganizeMode, moveLyricsSidecar bool) ([]Plan, error) {
	return p.PlanManyWithBasePath(ctx, tracks, "", mode, moveLyricsSidecar)
}

func (p *Planner) PlanManyWithBasePath(ctx context.Context, tracks []domain.Track, basePath string, mode domain.OrganizeMode, moveLyricsSidecar bool) ([]Plan, error) {
	plans := make([]Plan, 0, len(tracks))
	seen := make(map[string]int)
	for _, track := range tracks {
		plan, err := p.PlanWithBasePath(ctx, track, basePath, mode, moveLyricsSidecar)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(filepath.ToSlash(plan.Target))
		if index, found := seen[key]; found && plan.State == domain.OrganizeReady {
			plan.State = domain.OrganizeConflict
			plan.Warnings = append(plan.Warnings, "本批次中存在相同目标路径")
			plans[index].State = domain.OrganizeConflict
			plans[index].Warnings = append(plans[index].Warnings, "本批次中存在相同目标路径")
		} else if plan.State == domain.OrganizeReady {
			seen[key] = len(plans)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func NormalizeBasePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	return containedRelative(value)
}

func relativeWithinBase(relative, base string) bool {
	return base == "" || relative == base || strings.HasPrefix(relative, base+"/")
}

func (p *Planner) containedPath(relative string) (string, error) {
	clean, err := containedRelative(relative)
	if err != nil {
		return "", err
	}
	path := filepath.Join(p.root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(p.root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideRoot
	}
	return path, nil
}

func (p *Planner) checkSource(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrSourceMissing
	}
	if err != nil {
		return fmt.Errorf("检查源文件失败：%w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("源文件不是普通文件")
	}
	return nil
}

func (p *Planner) checkAncestors(path string) error {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		if current == p.root {
			return nil
		}
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("目标目录包含不安全路径")
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("检查目标目录失败：%w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ErrPathOutsideRoot
		}
	}
}

func containedRelative(value string) (string, error) {
	value = filepath.ToSlash(strings.TrimSpace(value))
	if value == "" || filepath.IsAbs(filepath.FromSlash(value)) {
		return "", ErrPathOutsideRoot
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrPathOutsideRoot
	}
	return clean, nil
}

func primaryArtist(values []string) string {
	value := ""
	for _, candidate := range values {
		if value = strings.TrimSpace(candidate); value != "" {
			break
		}
	}
	if value == "" {
		return ""
	}
	for _, separator := range []string{";", "；", "、", " / "} {
		if index := strings.Index(value, separator); index >= 0 {
			return strings.TrimSpace(value[:index])
		}
	}
	lower := strings.ToLower(value)
	for _, separator := range []string{" feat. ", " featuring ", " ft. "} {
		if index := strings.Index(lower, separator); index >= 0 {
			return strings.TrimSpace(value[:index])
		}
	}
	return value
}

func sanitizeSegment(value, fallback string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, runeValue := range value {
		if unicode.IsControl(runeValue) {
			continue
		}
		switch runeValue {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			builder.WriteRune('_')
		default:
			builder.WriteRune(runeValue)
		}
	}
	value = strings.TrimRight(builder.String(), " .")
	if value == "" || value == "." || value == ".." {
		value = fallback
	}
	reserved := strings.ToUpper(value)
	if reserved == "CON" || reserved == "PRN" || reserved == "AUX" || reserved == "NUL" || (len(reserved) == 4 && (strings.HasPrefix(reserved, "COM") || strings.HasPrefix(reserved, "LPT")) && reserved[3] >= '1' && reserved[3] <= '9') {
		value = "_" + value
	}
	for len([]byte(value)) > 180 {
		runes := []rune(value)
		value = string(runes[:len(runes)-1])
	}
	return value
}
