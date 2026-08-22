//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package filewrite

import (
	"io/fs"
	"os"
	"syscall"
)

func preserveOwnership(path string, info fs.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return os.Chown(path, int(stat.Uid), int(stat.Gid))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
