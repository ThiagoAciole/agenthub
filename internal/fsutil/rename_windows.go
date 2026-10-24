//go:build windows

package fsutil

import "syscall"

func renameNoReplace(oldPath, newPath string) error {
	return syscall.Rename(oldPath, newPath)
}
