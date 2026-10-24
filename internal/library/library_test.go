package library

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func createBareGitFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	repo, bare := t.TempDir(), filepath.Join(t.TempDir(), "fixture.git")
	for _, args := range [][]string{{"-C", repo, "init", "-b", "main"}, {"-C", repo, "config", "user.name", "AgentHub Test"}, {"-C", repo, "config", "user.email", "agenthub@example.invalid"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"), []byte("# Imported\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "assets", "guide.md"), []byte("reference"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scripts", "install.sh"), []byte("touch SHOULD_NOT_RUN\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", repo, "add", "."}, {"-C", repo, "commit", "-m", "fixture"}, {"clone", "--bare", repo, bare}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return bare
}

func TestGitAddPinsRevisionAndCopiesOnlySelectedTree(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "state")
	bare := createBareGitFixture(t)
	plan, err := PlanAddGit(context.Background(), root, bare, "HEAD", "", "skills", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.GitCommit) != 40 || plan.ContentHash == "" || plan.Name != "fixture" {
		t.Fatalf("Git plan did not pin source: %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); !os.IsNotExist(err) {
		t.Fatalf("Git preview wrote canonical content: %v", err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("Git preview wrote private state: %v", err)
	}
	added, err := ApplyAdd(context.Background(), plan, root)
	if err != nil {
		t.Fatal(err)
	}
	if added.Origin != "git" || added.Source == nil || added.Source.Commit != plan.GitCommit || added.Source.Ref != "HEAD" || added.SHA256 != plan.ContentHash {
		t.Fatalf("Git provenance was not recorded: %+v", added)
	}
	for name, want := range map[string]string{"SKILL.md": "# Imported\n", filepath.Join("assets", "guide.md"): "reference", filepath.Join("scripts", "install.sh"): "touch SHOULD_NOT_RUN\n"} {
		if got, err := os.ReadFile(filepath.Join(root, "skills", "fixture", name)); err != nil || string(got) != want {
			t.Fatalf("imported %s = %q, %v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "fixture", "SHOULD_NOT_RUN")); !os.IsNotExist(err) {
		t.Fatalf("import executed a script: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "fixture", ".git")); !os.IsNotExist(err) {
		t.Fatalf("Git metadata leaked into resource: %v", err)
	}
}

func TestGitAddRejectsRefDriftAfterPreview(t *testing.T) {
	root, bare := t.TempDir(), createBareGitFixture(t)
	plan, err := PlanAddGit(context.Background(), root, bare, "HEAD", "", "skills", "")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "source")
	if out, err := exec.Command("git", "clone", bare, repo).CombinedOutput(); err != nil {
		t.Fatalf("clone fixture: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"), []byte("# changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", repo, "add", "SKILL.md"}, {"-C", repo, "-c", "user.name=AgentHub Test", "-c", "user.email=agenthub@example.invalid", "commit", "-m", "changed"}, {"-C", repo, "push", "origin", "HEAD:main"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if _, err := ApplyAdd(context.Background(), plan, root); err == nil {
		t.Fatal("expected changed ref to invalidate reviewed Git plan")
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); !os.IsNotExist(err) {
		t.Fatalf("stale Git plan wrote canonical content: %v", err)
	}
}

func TestGitAddSelectsDirectoryOrSingleFile(t *testing.T) {
	root, bare := t.TempDir(), createBareGitFixture(t)
	dirPlan, err := PlanAddGit(context.Background(), root, bare, "main", "assets", "skills", "")
	if err != nil {
		t.Fatal(err)
	}
	if dirPlan.Name != "assets" || len(dirPlan.Files) != 1 || dirPlan.Files[0] != "guide.md" {
		t.Fatalf("subdirectory selection is wrong: %+v", dirPlan)
	}
	if _, err := ApplyAdd(context.Background(), dirPlan, root); err != nil {
		t.Fatal(err)
	}
	filePlan, err := PlanAddGit(context.Background(), root, bare, "main", "scripts/install.sh", "commands", "")
	if err != nil {
		t.Fatal(err)
	}
	if filePlan.Name != "install" || len(filePlan.Files) != 1 || filePlan.Files[0] != "install.sh" {
		t.Fatalf("single-file selection is wrong: %+v", filePlan)
	}
	if _, err := ApplyAdd(context.Background(), filePlan, root); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "commands", "install")); err != nil || string(got) != "touch SHOULD_NOT_RUN\n" {
		t.Fatalf("single-file import content missing: %q %v", got, err)
	}
}

func TestGitURLValidationRejectsCredentialsAndUnsafeProtocols(t *testing.T) {
	if got, origin, err := normalizeGitRepository("github:owner/repo"); err != nil || got != "https://github.com/owner/repo.git" || origin != got {
		t.Fatalf("GitHub shorthand normalization failed: %q %q %v", got, origin, err)
	}
	for _, input := range []string{"https://user:secret@example.test/repo.git", "https://example.test/repo.git?token=secret", "git://example.test/repo.git", "ext::sh -c unsafe"} {
		if _, _, err := normalizeGitRepository(input); err == nil {
			t.Errorf("accepted unsafe/credential-bearing source %q", input)
		}
	}
}

func TestLocalAddPlanRevalidatesSourceBeforeWrite(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanAddLocal(root, source, "skills", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); !os.IsNotExist(err) {
		t.Fatalf("local add preview wrote canonical data: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyAdd(context.Background(), plan, root); err == nil {
		t.Fatal("expected changed local source to invalidate plan")
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); !os.IsNotExist(err) {
		t.Fatalf("stale local plan wrote canonical data: %v", err)
	}
}

func TestRemovePlanIsReadOnlyAndCreatesRestorableTombstone(t *testing.T) {
	root, state, source := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "assets", "note.txt"), []byte("aux"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanRemove(root, entry.Name, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "backups")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote state: %v", err)
	}
	if _, err := os.Stat(plan.Target); err != nil {
		t.Fatalf("preview removed canonical data: %v", err)
	}
	b, _ := json.Marshal(plan)
	var decoded RemovePlan
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	removed, err := ApplyRemove(decoded, root, state)
	if err != nil {
		t.Fatal(err)
	}
	removedAgain, err := ApplyRemove(decoded, root, state)
	if err != nil || removedAgain.LastOperationID != decoded.PlanHash {
		t.Fatalf("reapplying identical remove plan was not idempotent: %+v %v", removedAgain, err)
	}
	if removed.ID != entry.ID || removed.Status != "tombstoned" || removed.DeletedAt == nil {
		t.Fatalf("missing stable tombstone: %+v", removed)
	}
	if _, err := os.Lstat(plan.Target); !os.IsNotExist(err) {
		t.Fatalf("canonical resource remains: %v", err)
	}
	live, err := List(root)
	if err != nil || len(live) != 0 {
		t.Fatalf("tombstone appears active: %v %+v", err, live)
	}
	trash := filepath.Join(state, filepath.FromSlash(removed.TrashSnapshot))
	if got, err := os.ReadFile(filepath.Join(trash, "assets", "note.txt")); err != nil || string(got) != "aux" {
		t.Fatalf("trash snapshot lost auxiliary content: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(trash, "SKILL.md")); err != nil || string(got) != "keep me" {
		t.Fatalf("trash snapshot lost resource: %q %v", got, err)
	}
}

func TestApplyRemoveRejectsDriftAfterPreview(t *testing.T) {
	root, state, source := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "prompt.md"), []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanRemove(root, entry.Name, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "prompts", entry.Name)
	if err := os.WriteFile(filepath.Join(target, "prompt.md"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRemove(plan, root, state); err == nil {
		t.Fatal("expected drift to block removal")
	}
	if got, err := os.ReadFile(filepath.Join(target, "prompt.md")); err != nil || string(got) != "external" {
		t.Fatalf("external edit was lost: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(state, "backups")); !os.IsNotExist(err) {
		t.Fatalf("failed remove wrote a snapshot: %v", err)
	}
}

func TestPlanRemoveRejectsSymlinkInsideResource(t *testing.T) {
	root, source, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "resource.md"), []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(root, "skills", entry.Name, "resource.md")
	if err := os.Remove(canonical); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), canonical); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := PlanRemove(root, entry.Name, entry.Type); err == nil {
		t.Fatal("remove plan followed a symlink inside the resource")
	}
}

func TestRestorePlanRequiresAbsentTargetAndCreatesNewRevision(t *testing.T) {
	root, state, source := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "prompt.md"), []byte("recoverable"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	removePlan, err := PlanRemove(root, entry.Name, entry.Type)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := ApplyRemove(removePlan, root, state)
	if err != nil {
		t.Fatal(err)
	}
	trashHistory, err := GetHistory(root, entry.Name, entry.Type)
	if err != nil || len(trashHistory.Revisions) != 1 || trashHistory.Revisions[0].Current || trashHistory.Revisions[0].Snapshot != removed.TrashSnapshot {
		t.Fatalf("tombstone history did not preserve its last revision: %+v %v", trashHistory, err)
	}
	plan, err := PlanRestore(root, state, entry.Name, entry.Type)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "prompts", entry.Name)); !os.IsNotExist(err) {
		t.Fatalf("restore preview created target: %v", err)
	}
	target := filepath.Join(root, "prompts", entry.Name)
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "third-party.txt"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRestore(plan, root, state); err == nil {
		t.Fatal("restore overwrote a destination created after preview")
	}
	if got, err := os.ReadFile(filepath.Join(target, "third-party.txt")); err != nil || string(got) != "preserve" {
		t.Fatalf("restore lost unowned destination: %q %v", got, err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	restored, err := ApplyRestore(plan, root, state)
	if err != nil {
		t.Fatal(err)
	}
	restoredAgain, err := ApplyRestore(plan, root, state)
	if err != nil || len(restoredAgain.Revisions) != len(restored.Revisions) {
		t.Fatalf("reapplying identical restore plan created another revision: %+v %v", restoredAgain, err)
	}
	if restored.ID != entry.ID || restored.Status != "" || restored.DeletedAt != nil || len(restored.Revisions) != len(removed.Revisions)+1 {
		t.Fatalf("restore did not preserve identity/new revision: %+v", restored)
	}
	if got, err := os.ReadFile(filepath.Join(root, "prompts", entry.Name, "prompt.md")); err != nil || string(got) != "recoverable" {
		t.Fatalf("restored content missing: %q %v", got, err)
	}
	activeHistory, err := GetHistory(root, entry.Name, entry.Type)
	if err != nil || len(activeHistory.Revisions) != 2 || !activeHistory.Revisions[1].Current || activeHistory.Revisions[0].ID == activeHistory.Revisions[1].ID {
		t.Fatalf("restore did not append a new immutable revision: %+v %v", activeHistory, err)
	}
}

func TestAddCopiesResourceAndRejectsDuplicate(t *testing.T) {
	root, src := t.TempDir(), t.TempDir()
	externalRegistry := filepath.Join(root, "agents.lock.json")
	if err := os.WriteFile(externalRegistry, []byte("external-owner"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "assets", "sample.txt"), []byte("asset"), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := Add(root, src, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if e.SHA256 == "" {
		t.Fatal("missing content hash")
	}
	if e.Origin != "local" {
		t.Fatalf("portable origin exposed a local path: %q", e.Origin)
	}
	if b, err := os.ReadFile(filepath.Join(root, ".agenthub", "registry.json")); err != nil || !strings.Contains(string(b), e.ID) || strings.Contains(string(b), src) {
		t.Fatalf("AgentHub registry missing: %v", err)
	}
	if b, err := os.ReadFile(externalRegistry); err != nil || string(b) != "external-owner" {
		t.Fatalf("external registry changed: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "skills", filepath.Base(src), "assets", "sample.txt")); err != nil || string(b) != "asset" {
		t.Fatalf("asset was not preserved: %q %v", b, err)
	}
	if _, err = Add(root, src, "skills"); err == nil {
		t.Fatal("expected duplicate rejection")
	}
}

func TestAddRejectsSymlink(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	target := filepath.Join(dir, "outside.md")
	if err := os.WriteFile(target, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Add(root, link, "skills"); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestUpdatePreservesIdentityAndSnapshotsPreviousRevision(t *testing.T) {
	root, state, fixtureRoot := t.TempDir(), t.TempDir(), t.TempDir()
	source, updateSource := filepath.Join(fixtureRoot, "source"), filepath.Join(fixtureRoot, "update")
	for _, dir := range []string{source, updateSource} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(source, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "assets", "keep.txt"), []byte("v1 asset"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := Add(root, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(updateSource, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateSource, "SKILL.md"), []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateSource, "assets", "new.txt"), []byte("new asset"), 0600); err != nil {
		t.Fatal(err)
	}
	updated, err := Update(root, state, "source", updateSource, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID || updated.SHA256 == first.SHA256 || len(updated.Revisions) != 1 || updated.Revisions[0].SHA256 != first.SHA256 {
		t.Fatalf("update lost resource identity/history: first=%+v updated=%+v", first, updated)
	}
	oldSnapshot := filepath.Join(state, filepath.FromSlash(updated.Revisions[0].Snapshot))
	if got, err := os.ReadFile(filepath.Join(oldSnapshot, "SKILL.md")); err != nil || string(got) != "v1" {
		t.Fatalf("previous revision snapshot missing: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "skills", "source", "SKILL.md")); err != nil || string(got) != "v2" {
		t.Fatalf("canonical skill not updated: %q %v", got, err)
	}
}

func TestHistoryShowsCurrentAndImmutableRevisions(t *testing.T) {
	root, state, source := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "prompt.md"), []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := GetHistory(root, entry.Name, entry.Type)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Revisions) != 1 || !initial.Revisions[0].Current || initial.Revisions[0].SHA256 != entry.SHA256 {
		t.Fatalf("initial history missing current revision: %+v", initial)
	}
	updateSource := filepath.Join(t.TempDir(), "v2.md")
	if err := os.WriteFile(updateSource, []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	updated, err := Update(root, state, entry.Name, updateSource, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	history, err := GetHistory(root, entry.Name, entry.Type)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Revisions) != 2 || history.Revisions[0].Current || !history.Revisions[1].Current || history.Revisions[1].SHA256 != updated.SHA256 {
		t.Fatalf("history did not expose old and current versions: %+v", history)
	}
	if history.Revisions[0].ID == "" || history.Revisions[1].ID == "" || history.Revisions[0].ID == history.Revisions[1].ID {
		t.Fatalf("revision identity missing or duplicated: %+v", history.Revisions)
	}
}

func TestUpdateRejectsDriftAndPreservesCanonicalBytes(t *testing.T) {
	root, state, source := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(root, "skills", entry.Name, "SKILL.md")
	if err := os.WriteFile(canonical, []byte("external edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(root, state, entry.Name, source, "skills"); err == nil {
		t.Fatal("expected drifted canonical content to block update")
	}
	if got, err := os.ReadFile(canonical); err != nil || string(got) != "external edit" {
		t.Fatalf("external edit was lost: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(state, "backups")); !os.IsNotExist(err) {
		t.Fatalf("update created a backup before validating ownership: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("fixture missing resource identity")
	}
}

func TestRenamePreservesResourceAndRevisionIdentity(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("portable skill"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID == "skills:"+entry.Name {
		t.Fatalf("new resource ID still depends on name: %q", entry.ID)
	}
	plan, err := PlanRename(root, entry.Name, "skills", "renamed-skill")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := ApplyRename(plan, root)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != entry.ID || renamed.Name != "renamed-skill" || renamed.CurrentRevision.ID != entry.CurrentRevision.ID {
		t.Fatalf("rename changed identity or revision: before=%+v after=%+v", entry, renamed)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", entry.Name)); !os.IsNotExist(err) {
		t.Fatalf("old canonical path remains: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "skills", renamed.Name, "SKILL.md")); err != nil || string(got) != "portable skill" {
		t.Fatalf("renamed content mismatch: %q %v", got, err)
	}
}

func TestRenameRejectsContentChangedAfterPlanAndPreservesLegacyID(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	entry.ID = "skills:" + entry.Name
	if err := writeEntries(root, []Entry{entry}); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanRename(root, entry.Name, "skills", "renamed-skill")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", entry.Name, "SKILL.md"), []byte("external edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRename(plan, root); err == nil {
		t.Fatal("rename accepted content changed after preview")
	}
	if got, err := os.ReadFile(filepath.Join(root, "skills", entry.Name, "SKILL.md")); err != nil || string(got) != "external edit" {
		t.Fatalf("rename lost the changed source: %q %v", got, err)
	}
}

func TestUpdateSnapshotsFlatPromptBundle(t *testing.T) {
	root, state, sourceRoot := t.TempDir(), t.TempDir(), t.TempDir()
	oldSource := filepath.Join(sourceRoot, "old.md")
	newSource := filepath.Join(sourceRoot, "new.md")
	if err := os.WriteFile(oldSource, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newSource, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, oldSource, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Update(root, state, entry.Name, newSource, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(state, filepath.FromSlash(updated.Revisions[0].Snapshot))
	if got, err := os.ReadFile(filepath.Join(snapshot, "old.md")); err != nil || string(got) != "old" {
		t.Fatalf("flat prompt snapshot missing: %q %v", got, err)
	}
}

func TestUpdatePlanIsReadOnlyAndApplyRejectsChangedSource(t *testing.T) {
	root, state, fixtureRoot := t.TempDir(), t.TempDir(), t.TempDir()
	source := filepath.Join(fixtureRoot, "skill")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := Add(root, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	updateSource := filepath.Join(fixtureRoot, "next")
	if err := os.Mkdir(updateSource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateSource, "SKILL.md"), []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanUpdate(root, entry.Name, updateSource, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != "update" || plan.BeforeHash != entry.SHA256 || plan.AfterHash == entry.SHA256 {
		t.Fatalf("unexpected update plan: %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(state, "backups")); !os.IsNotExist(err) {
		t.Fatalf("read-only plan wrote a backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(updateSource, "SKILL.md"), []byte("v3"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUpdate(plan, root, state); err == nil {
		t.Fatal("expected apply to reject a source changed after planning")
	}
	if got, err := os.ReadFile(filepath.Join(root, "skills", entry.Name, "SKILL.md")); err != nil || string(got) != "v1" {
		t.Fatalf("stale update plan changed canonical content: %q %v", got, err)
	}
}
