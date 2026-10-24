package fsutil

import "fmt"

func RenameNoReplace(oldPath, newPath string) error {
	if err := renameNoReplace(oldPath, newPath); err != nil {
		return fmt.Errorf("rename %s to unused path %s: %w", oldPath, newPath, err)
	}
	return nil
}
