package registry

import (
	"github.com/ThiagoAciole/agenthub/internal/models"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildMarksContentDuplicates(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("same"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result := Build([]models.Item{{Provider: "codex", Type: models.Agent, Name: "a", Path: a}, {Provider: "claude-code", Type: models.Agent, Name: "b", Path: b}})
	if len(result.DuplicateGroups) != 1 || len(result.Items[0].Duplicates) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
