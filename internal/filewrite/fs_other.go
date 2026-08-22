//go:build windows || plan9

package filewrite

import "io/fs"

func preserveOwnership(string, fs.FileInfo) error { return nil }

func syncDirectory(string) error { return nil }
