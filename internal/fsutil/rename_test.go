package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameNoReplacePreservesExistingEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "data"), []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := RenameNoReplace(source, target); err == nil {
		t.Fatal("rename replaced an existing empty directory")
	}
	if _, err := os.Stat(filepath.Join(source, "data")); err != nil {
		t.Fatalf("source disappeared after rejected rename: %v", err)
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Fatalf("existing destination changed: entries=%v err=%v", entries, err)
	}
}
