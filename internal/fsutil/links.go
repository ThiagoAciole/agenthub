package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

func IsLinkOrJunction(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0 || isWindowsJunction(info)
}

func RejectLinkPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := filepath.Clean(abs); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && IsLinkOrJunction(info) {
			return fmt.Errorf("symlink ou junction não permitido no caminho: %s", current)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
