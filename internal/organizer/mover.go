package organizer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/mutation"
)

// Move executes one already planned move. It rechecks the source and target
// immediately before renaming and rolls back a sidecar move if the audio move
// cannot be completed.
func Move(ctx context.Context, plan Plan) error {
	if plan.State == domain.OrganizeNoop {
		return nil
	}
	if plan.State != domain.OrganizeReady {
		return fmt.Errorf("cannot move plan in state %s", plan.State)
	}
	unlock := mutation.Acquire(plan.SourceAbs)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if info, err := os.Lstat(plan.SourceAbs); err != nil {
		return fmt.Errorf("%w: %v", ErrSourceMissing, err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("源文件不是普通文件")
	}
	if _, err := os.Lstat(plan.TargetAbs); err == nil {
		return ErrTargetExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("检查目标文件失败：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.TargetAbs), 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败：%w", err)
	}
	sidecarMoved := false
	sidecarSourceAbs := strings.TrimSuffix(plan.SourceAbs, filepath.Ext(plan.SourceAbs)) + ".lrc"
	sidecarTargetAbs := strings.TrimSuffix(plan.TargetAbs, filepath.Ext(plan.TargetAbs)) + ".lrc"
	if plan.SidecarExists {
		if _, err := os.Lstat(sidecarTargetAbs); err == nil {
			return fmt.Errorf("目标歌词 sidecar 已存在")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("检查目标歌词 sidecar 失败：%w", err)
		}
		if err := os.Rename(sidecarSourceAbs, sidecarTargetAbs); err != nil {
			return fmt.Errorf("移动歌词 sidecar 失败：%w", err)
		}
		sidecarMoved = true
	}
	rollbackSidecar := func() {
		if sidecarMoved {
			_ = os.Rename(sidecarTargetAbs, sidecarSourceAbs)
		}
	}
	if err := ctx.Err(); err != nil {
		rollbackSidecar()
		return err
	}
	if err := os.Rename(plan.SourceAbs, plan.TargetAbs); err != nil {
		rollbackSidecar()
		return fmt.Errorf("移动音频文件失败：%w", err)
	}
	return nil
}

// Rollback restores a completed move. It is used only when the index update
// after a successful filesystem move cannot be persisted.
func Rollback(ctx context.Context, plan Plan) error {
	unlock := mutation.Acquire(plan.SourceAbs)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Lstat(plan.TargetAbs); err != nil {
		return fmt.Errorf("rollback target missing: %w", err)
	}
	if _, err := os.Lstat(plan.SourceAbs); err == nil {
		return fmt.Errorf("rollback source already exists")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check rollback source: %w", err)
	}
	if err := os.Rename(plan.TargetAbs, plan.SourceAbs); err != nil {
		return fmt.Errorf("rollback audio file: %w", err)
	}
	if plan.SidecarExists {
		sidecarSourceAbs := strings.TrimSuffix(plan.SourceAbs, filepath.Ext(plan.SourceAbs)) + ".lrc"
		sidecarTargetAbs := strings.TrimSuffix(plan.TargetAbs, filepath.Ext(plan.TargetAbs)) + ".lrc"
		if err := os.Rename(sidecarTargetAbs, sidecarSourceAbs); err != nil {
			return fmt.Errorf("rollback lyrics sidecar: %w", err)
		}
	}
	return nil
}
