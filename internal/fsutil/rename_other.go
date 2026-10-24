//go:build !linux && !windows && !darwin

package fsutil

import "fmt"

func renameNoReplace(string, string) error {
	return fmt.Errorf("rename sem substituição não é implementado neste sistema")
}
