package cloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ThiagoAciole/agenthub/internal/library"
)

func TestApplyRemoteDecisionUpdatesAndReplaysIdempotently(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	local := addSkill(t, root, "shared", "local v1\n")
	base, err := BuildSnapshot(root, filepath.Join(t.TempDir(), "base"), map[string]bool{local.ID: true})
	if err != nil {
		t.Fatal(err)
	}

	remoteRoot := t.TempDir()
	remote := addSkill(t, remoteRoot, "shared", "remote v2\n")
	candidatePath := filepath.Join(t.TempDir(), "candidate")
	remoteSnapshot, err := BuildSnapshot(remoteRoot, candidatePath, map[string]bool{remote.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	remoteSnapshot.Resources[0].ResourceID = local.ID
	remoteSnapshot.Resources[0].RevisionID = "remote-revision-2"
	manifest, err := json.MarshalIndent(remoteSnapshot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidatePath, "agenthub.manifest.json"), append(manifest, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	localSnapshot := base
	plan, err := PlanSync(base, localSnapshot, remoteSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := ApplyRemoteDecision(root, state, candidatePath, plan, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != local.ID || library.CurrentRevision(updated).ID != "remote-revision-2" || updated.Origin != "cloud" {
		t.Fatalf("remote identity was not preserved: %+v", updated)
	}
	if got, err := os.ReadFile(filepath.Join(root, "skills", "shared", "SKILL.md")); err != nil || string(got) != "remote v2\n" {
		t.Fatalf("remote content not materialized: %q %v", got, err)
	}
	if len(updated.Revisions) != 1 {
		t.Fatalf("previous revision was not backed up: %+v", updated.Revisions)
	}
	if got, err := os.ReadFile(filepath.Join(state, filepath.FromSlash(updated.Revisions[0].Snapshot), "SKILL.md")); err != nil || string(got) != "local v1\n" {
		t.Fatalf("previous revision backup missing: %q %v", got, err)
	}
	replayed, err := ApplyRemoteDecision(root, state, candidatePath, plan, local.ID)
	if err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	if library.CurrentRevision(replayed).ID != "remote-revision-2" || len(replayed.Revisions) != 1 {
		t.Fatalf("replay created an extra revision: %+v", replayed)
	}
}

func TestApplyRemoteDecisionRejectsStaleLocalState(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	local := addSkill(t, root, "shared", "local v1\n")
	base, err := BuildSnapshot(root, filepath.Join(t.TempDir(), "base"), map[string]bool{local.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	remoteRoot := t.TempDir()
	remote := addSkill(t, remoteRoot, "shared", "remote v2\n")
	candidate := filepath.Join(t.TempDir(), "candidate")
	remoteSnapshot, err := BuildSnapshot(remoteRoot, candidate, map[string]bool{remote.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	remoteSnapshot.Resources[0].ResourceID = local.ID
	remoteSnapshot.Resources[0].RevisionID = "remote-revision-2"
	manifest, _ := json.MarshalIndent(remoteSnapshot, "", "  ")
	if err := os.WriteFile(filepath.Join(candidate, "agenthub.manifest.json"), append(manifest, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSync(base, base, remoteSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "skills", "shared", "SKILL.md")
	if err := os.WriteFile(path, []byte("external edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRemoteDecision(root, state, candidate, plan, local.ID); err == nil {
		t.Fatal("stale local content was overwritten")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "external edit\n" {
		t.Fatalf("stale bytes changed: %q %v", got, err)
	}
}
