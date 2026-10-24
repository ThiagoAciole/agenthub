//go:build darwin

package fsutil

import (
	"runtime"
	"syscall"
	"unsafe"
)

func renameNoReplace(oldPath, newPath string) error {
	old, err := syscall.BytePtrFromString(oldPath)
	if err != nil {
		return err
	}
	new, err := syscall.BytePtrFromString(newPath)
	if err != nil {
		return err
	}
	const renameatxNP = uintptr(0x2000000 + 488)
	const renameExclusive = uintptr(0x4)
	fd := ^uintptr(1) // AT_FDCWD (-2)
	_, _, errno := syscall.Syscall6(renameatxNP, fd, uintptr(unsafe.Pointer(old)), fd, uintptr(unsafe.Pointer(new)), renameExclusive, 0)
	runtime.KeepAlive(old)
	runtime.KeepAlive(new)
	if errno != 0 {
		return errno
	}
	return nil
}
