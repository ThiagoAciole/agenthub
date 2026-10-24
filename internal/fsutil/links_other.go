//go:build !windows

package fsutil

import "os"

func isWindowsJunction(os.FileInfo) bool { return false }
