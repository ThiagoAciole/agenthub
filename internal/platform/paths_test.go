package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSeparatesCanonicalAndPrivateRoots(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "projeto com ç")
	paths, err := New().Resolve(project)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Library != filepath.Join(home, ".agents") || paths.State != filepath.Join(home, ".agenthub") {
		t.Fatalf("unexpected home roots: %+v", paths)
	}
	absProject, err := filepath.Abs(project)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Project != absProject || paths.ProjectLibrary != filepath.Join(absProject, ".agents") {
		t.Fatalf("unexpected project roots: %+v", paths)
	}
}
