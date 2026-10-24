package cloud

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ThiagoAciole/agenthub/internal/library"
)

func addSkill(t *testing.T, root, name, body string) library.Entry {
	t.Helper()
	source := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := library.Add(root, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestBuildSnapshotExportsOnlySelectedRegisteredResources(t *testing.T) {
	root, staging := t.TempDir(), filepath.Join(t.TempDir(), "git", "worktree")
	selected := addSkill(t, root, "selected", "portable instructions\n")
	unselected := addSkill(t, root, "unselected", "private local content\n")
	snapshot, err := BuildSnapshot(root, staging, map[string]bool{selected.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Resources) != 1 || snapshot.Resources[0].ResourceID != selected.ID || snapshot.Resources[0].Hash != selected.SHA256 {
		t.Fatalf("snapshot metadata does not match selected resource: %+v", snapshot)
	}
	if got, err := os.ReadFile(filepath.Join(staging, "skills", selected.Name, "SKILL.md")); err != nil || string(got) != "portable instructions\n" {
		t.Fatalf("selected content missing from staging: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(staging, "skills", unselected.Name)); !os.IsNotExist(err) {
		t.Fatalf("unselected content entered staging: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("snapshot initialized Git in canonical library: %v", err)
	}
}

func TestBuildSnapshotBlocksSecretsAndCanonicalDrift(t *testing.T) {
	root := t.TempDir()
	secret := addSkill(t, root, "secret", "api_key=supersecretvalue123\n")
	secretStage := filepath.Join(t.TempDir(), "secret-stage")
	if _, err := BuildSnapshot(root, secretStage, map[string]bool{secret.ID: true}); err == nil {
		t.Fatal("snapshot exported a detected API key")
	}
	if _, err := os.Lstat(secretStage); !os.IsNotExist(err) {
		t.Fatalf("failed secret scan published staging directory: %v", err)
	}
	drifted := addSkill(t, root, "drifted", "approved\n")
	if err := os.WriteFile(filepath.Join(root, "skills", drifted.Name, "SKILL.md"), []byte("changed after approval\n"), 0600); err != nil {
		t.Fatal(err)
	}
	driftStage := filepath.Join(t.TempDir(), "drift-stage")
	if _, err := BuildSnapshot(root, driftStage, map[string]bool{drifted.ID: true}); err == nil {
		t.Fatal("snapshot accepted canonical content that differs from registry hash")
	}
	if _, err := os.Lstat(driftStage); !os.IsNotExist(err) {
		t.Fatalf("failed drift check published staging directory: %v", err)
	}
}

func TestBuildSnapshotAllowsEnvironmentReferences(t *testing.T) {
	root := t.TempDir()
	entry := addSkill(t, root, "env-ref", "api_key=${AGENTHUB_TEST_TOKEN}\n")
	staging := filepath.Join(t.TempDir(), "stage")
	if _, err := BuildSnapshot(root, staging, map[string]bool{entry.ID: true}); err != nil {
		t.Fatalf("environment reference was treated as a plaintext secret: %v", err)
	}
}

func TestBuildSnapshotCarriesTombstoneWithoutDeletedContent(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	entry := addSkill(t, root, "removed", "old content\n")
	plan, err := library.PlanRemove(root, entry.Name, entry.Type)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := library.ApplyRemove(plan, root, state)
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(t.TempDir(), "stage")
	snapshot, err := BuildSnapshot(root, staging, map[string]bool{entry.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	resource := snapshot.Resources[0]
	if !resource.Tombstone || resource.ResourceID != removed.ID {
		t.Fatalf("tombstone was not represented in snapshot: %+v", resource)
	}
	if _, err := os.Stat(filepath.Join(staging, "skills", entry.Name)); !os.IsNotExist(err) {
		t.Fatalf("deleted content entered sync snapshot: %v", err)
	}
}

func TestBuildSnapshotRequiresExplicitSelectionAndNoReplaceDestination(t *testing.T) {
	root, destination := t.TempDir(), filepath.Join(t.TempDir(), "stage")
	entry := addSkill(t, root, "skill", "portable\n")
	if _, err := BuildSnapshot(root, destination, nil); err == nil {
		t.Fatal("snapshot allowed an implicit export")
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSnapshot(root, destination, map[string]bool{entry.ID: true}); err == nil {
		t.Fatal("snapshot replaced an existing staging destination")
	}
	if entries, err := os.ReadDir(destination); err != nil || len(entries) != 0 {
		t.Fatalf("existing destination changed: entries=%v err=%v", entries, err)
	}
}
