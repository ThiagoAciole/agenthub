package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFlatPromptUsesBundleStorageAndHashAcrossUpdate(t *testing.T) {
	root, state, source := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(source, []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanAddLocal(root, source, "prompts", "")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ApplyAdd(context.Background(), plan, root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "prompts", "review")
	if got, err := os.ReadFile(filepath.Join(target, "review.md")); err != nil || string(got) != "v1" {
		t.Fatalf("flat prompt was not stored as a Markdown bundle: %q %v", got, err)
	}
	if hash, err := hashTree(target); err != nil || hash != entry.SHA256 {
		t.Fatalf("stored bundle hash differs from registry: %q %v; want %q", hash, err, entry.SHA256)
	}
	updatedSource := filepath.Join(t.TempDir(), "review-next.md")
	if err := os.WriteFile(updatedSource, []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	update, err := PlanUpdate(root, entry.Name, updatedSource, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := ApplyUpdate(update, root, state)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "review-next.md")); err != nil || string(got) != "v2" {
		t.Fatalf("flat prompt update was not stored as a Markdown bundle: %q %v", got, err)
	}
	if hash, err := hashTree(target); err != nil || hash != updated.SHA256 {
		t.Fatalf("updated bundle hash differs from registry: %q %v; want %q", hash, err, updated.SHA256)
	}
}
