package library

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericwyn/tagger/internal/domain"
)

const maxProbeEntries = 20000

type DirectoryProbe struct {
	Path       string         `json:"path"`
	Name       string         `json:"name"`
	Readable   bool           `json:"readable"`
	Writable   bool           `json:"writable"`
	AudioFiles int            `json:"audioFiles"`
	Folders    int            `json:"folders"`
	Formats    map[string]int `json:"formats"`
	Warnings   []string       `json:"warnings,omitempty"`
}

// ProbeRoot validates a candidate music directory without changing the
// active library index. It deliberately counts only supported audio
// formats and never follows symlinks, matching Scanner's safety boundary.
func ProbeRoot(root string) (DirectoryProbe, error) {
	probe, err := ValidateRoot(root)
	if err != nil {
		return DirectoryProbe{}, err
	}
	abs := probe.Path
	entries := 0
	err = filepath.WalkDir(abs, func(path string, entry fs.DirEntry, walkErr error) error {
		entries++
		if entries > maxProbeEntries {
			return fs.SkipAll
		}
		if walkErr != nil {
			probe.Warnings = append(probe.Warnings, walkErr.Error())
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path != abs && entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			probe.Folders++
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if format, ok := domain.TrackFormatFromExtension(filepath.Ext(entry.Name())); ok {
			probe.AudioFiles++
			probe.Formats[string(format)]++
		}
		return nil
	})
	if err != nil && err != fs.SkipAll {
		return DirectoryProbe{}, fmt.Errorf("probe directory: %w", err)
	}
	if entries > maxProbeEntries {
		probe.Warnings = append(probe.Warnings, fmt.Sprintf("目录超过探测上限 %d 项，结果可能不完整", maxProbeEntries))
	}
	return probe, nil
}

// ValidateRoot performs only the cheap safety checks needed before enqueueing
// a scan. Callers that need counts should use ProbeRoot; registration and
// switching should avoid traversing a large library twice.
func ValidateRoot(root string) (DirectoryProbe, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return DirectoryProbe{}, fmt.Errorf("directory path is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return DirectoryProbe{}, fmt.Errorf("resolve directory path: %w", err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return DirectoryProbe{}, fmt.Errorf("stat directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return DirectoryProbe{}, fmt.Errorf("directory root must not be a symlink")
	}
	if !info.IsDir() {
		return DirectoryProbe{}, fmt.Errorf("path is not a directory")
	}
	probe := DirectoryProbe{Path: abs, Name: filepath.Base(abs), Formats: map[string]int{
		string(domain.FormatMP3):  0,
		string(domain.FormatFLAC): 0,
		string(domain.FormatWAV):  0,
		string(domain.FormatOGG):  0,
	}}
	if file, openErr := os.Open(abs); openErr == nil {
		probe.Readable = true
		_ = file.Close()
	} else {
		probe.Warnings = append(probe.Warnings, "目录不可读取："+openErr.Error())
	}
	probe.Writable = info.Mode().Perm()&0o222 != 0
	if !probe.Writable {
		probe.Warnings = append(probe.Warnings, "目录权限位显示为只读")
	}
	return probe, nil
}
