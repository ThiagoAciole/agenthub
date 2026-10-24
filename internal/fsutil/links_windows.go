//go:build windows

package fsutil

import (
	"os"
	"syscall"
)

func isWindowsJunction(info os.FileInfo) bool {
	// Go reports Windows mount-point reparse points as ModeIrregular.
	if info.Mode()&os.ModeIrregular == 0 {
		return false
	}
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && attributes.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
