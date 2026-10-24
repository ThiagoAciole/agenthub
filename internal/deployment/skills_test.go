package deployment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ThiagoAciole/agenthub/internal/fsutil"
	lib "github.com/ThiagoAciole/agenthub/internal/library"
)

func writeSkill(t *testing.T, root string) {
	t.Helper()
	skill := filepath.Join(root, "skills", "helper")
	if err := os.MkdirAll(filepath.Join(skill, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# helper\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "assets", "example.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
}

func canonicalSkill(t *testing.T) string {
	t.Helper()
	root, source := t.TempDir(), t.TempDir()
	writeSkill(t, source)
	if _, err := lib.Add(root, filepath.Join(source, "skills", "helper"), "skills"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestApplyCopiesSkillAndIsIdempotent(t *testing.T) {
	library, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(library, "helper", "claude-code", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Apply(plan, library, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Apply(plan, library, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("repeat apply created a second deployment: %q != %q", first.ID, second.ID)
	}
	for path, want := range map[string]string{"SKILL.md": "# helper\n", filepath.Join("assets", "example.txt"): "fixture"} {
		got, err := os.ReadFile(filepath.Join(targetRoot, "helper", path))
		if err != nil || string(got) != want {
			t.Fatalf("deployed %s = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestApplyRecordsOperationStatus(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "claude-code", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, libraryRoot, targetRoot, state); err != nil {
		t.Fatal(err)
	}
	operation, err := ReadOperation(state, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Version != 2 || operation.Status != "applied" || operation.IdempotencyKey != plan.PlanHash || operation.RevisionHash != plan.ContentHash {
		t.Fatalf("unexpected operation record: %+v", operation)
	}
	operation.Status = "applying"
	if err := writeOperation(state, operation); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, libraryRoot, targetRoot, state); err != nil {
		t.Fatalf("retry did not reconcile a committed operation: %v", err)
	}
	operation, err = ReadOperation(state, plan.PlanHash)
	if err != nil || operation.Status != "applied" {
		t.Fatalf("committed operation was not reconciled: %+v, %v", operation, err)
	}
}

func TestApplyRecordsPreWriteFailure(t *testing.T) {
	libraryRoot, state := canonicalSkill(t), t.TempDir()
	targetParent := t.TempDir()
	targetRoot := filepath.Join(targetParent, "provider-skills")
	if err := os.WriteFile(targetRoot, []byte("blocks directory creation"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSkill(libraryRoot, "helper", "claude-code", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, libraryRoot, targetRoot, state); err == nil {
		t.Fatal("expected apply to fail because provider root is a file")
	}
	operation, err := ReadOperation(state, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != "failed" || operation.Error == "" {
		t.Fatalf("failed apply was not journaled: %+v", operation)
	}
	if err := os.Remove(targetRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(targetRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, libraryRoot, targetRoot, state); err != nil {
		t.Fatalf("safe retry after pre-write failure failed: %v", err)
	}
	operation, err = ReadOperation(state, plan.PlanHash)
	if err != nil || operation.Status != "applied" || operation.Attempts != 2 {
		t.Fatalf("retry was not recorded: %+v, %v", operation, err)
	}
}

func TestApplyRecoversWhenSwapAndRollbackRenameFail(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	firstPlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(firstPlan, libraryRoot, targetRoot, state); err != nil {
		t.Fatal(err)
	}
	updatedSource := filepath.Join(t.TempDir(), "helper-v2")
	if err := os.Mkdir(updatedSource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updatedSource, "SKILL.md"), []byte("# helper v2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Update(libraryRoot, state, "helper", updatedSource, "skills"); err != nil {
		t.Fatal(err)
	}
	updatePlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	rename := func(oldPath, newPath string) error {
		if newPath == updatePlan.TargetPath {
			return errors.New("injected rename failure")
		}
		return os.Rename(oldPath, newPath)
	}
	if _, err := applyWithRename(updatePlan, libraryRoot, targetRoot, state, rename); err == nil {
		t.Fatal("expected staged swap and rollback to fail")
	}
	operation, err := ReadOperation(state, updatePlan.PlanHash)
	if err != nil || operation.Status != "partial" || operation.RecoveryPath == "" {
		t.Fatalf("partial operation was not recoverable: %+v %v", operation, err)
	}
	if _, err := os.Stat(updatePlan.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("failed swap unexpectedly created destination: %v", err)
	}
	recoveryPlan, err := PlanOperationRecovery(state, operation.ID, targetRoot)
	if err != nil || recoveryPlan.Action != "restore_base" {
		t.Fatalf("recovery did not select verified prior revision: %+v %v", recoveryPlan, err)
	}
	if _, err := ApplyOperationRecovery(recoveryPlan, targetRoot, state); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(updatePlan.TargetPath, "SKILL.md"))
	if err != nil || string(got) != "# helper\n" {
		t.Fatalf("previous revision was not restored: %q %v", got, err)
	}
}

func TestConcurrentApplyDoesNotCorruptOperationJournal(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "claude-code", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := Apply(plan, libraryRoot, targetRoot, state)
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded == 0 {
		t.Fatal("neither concurrent apply succeeded")
	}
	operation, err := ReadOperation(state, plan.PlanHash)
	if err != nil || operation.Status != "applied" {
		t.Fatalf("concurrent apply corrupted operation journal: %+v %v", operation, err)
	}
	deployments, err := ListDeployments(state, "all")
	if err != nil || len(deployments) != 1 {
		t.Fatalf("concurrent apply created duplicate deployments: %+v %v", deployments, err)
	}
}

func TestReadOperationRejectsOversizedJournal(t *testing.T) {
	state, id := t.TempDir(), strings.Repeat("a", 64)
	path := filepath.Join(state, "operations", id+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOperation(state, id); err == nil {
		t.Fatal("oversized operation journal was accepted")
	}
}

func TestRecoverPartialCreateFinalizesVerifiedDeployment(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "opencode", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := startOperation(state, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.TargetPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(plan.TargetPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(plan.SourceRoot, plan.TargetPath); err != nil {
		t.Fatal(err)
	}
	operation.Status = "partial"
	if err := writeOperation(state, operation); err != nil {
		t.Fatal(err)
	}
	recoveryPlan, err := PlanOperationRecovery(state, operation.ID, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if recoveryPlan.Action != "finalize" {
		t.Fatalf("action=%q; want finalize", recoveryPlan.Action)
	}
	recovered, err := ApplyOperationRecovery(recoveryPlan, targetRoot, state)
	if err != nil || recovered.Status != "applied" {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	if _, err := findDeploymentByID(state, plan.PlanHash); err != nil {
		t.Fatalf("deployment manifest was not recovered: %v", err)
	}
	if _, err := ApplyOperationRecovery(recoveryPlan, targetRoot, state); err != nil {
		t.Fatalf("repeated recovery was not idempotent: %v", err)
	}
}

func TestRecoverPartialUpdateRestoresVerifiedBase(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	firstPlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(firstPlan, libraryRoot, targetRoot, state); err != nil {
		t.Fatal(err)
	}
	updatedSource := filepath.Join(t.TempDir(), "helper-v2")
	if err := os.Mkdir(updatedSource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updatedSource, "SKILL.md"), []byte("# helper v2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Update(libraryRoot, state, "helper", updatedSource, "skills"); err != nil {
		t.Fatal(err)
	}
	updatePlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := startOperation(state, updatePlan)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(state, filepath.FromSlash(operation.Snapshot))
	if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(updatePlan.TargetPath, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(updatePlan.TargetPath); err != nil {
		t.Fatal(err)
	}
	operation.Status = "partial"
	if err := writeOperation(state, operation); err != nil {
		t.Fatal(err)
	}
	recoveryPlan, err := PlanOperationRecovery(state, operation.ID, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if recoveryPlan.Action != "restore_base" {
		t.Fatalf("action=%q; want restore_base", recoveryPlan.Action)
	}
	recovered, err := ApplyOperationRecovery(recoveryPlan, targetRoot, state)
	if err != nil || recovered.Status != "recovered" {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	if got, err := os.ReadFile(filepath.Join(updatePlan.TargetPath, "SKILL.md")); err != nil || string(got) != "# helper\n" {
		t.Fatalf("base was not restored: %q %v", got, err)
	}
	if _, err := ApplyOperationRecovery(recoveryPlan, targetRoot, state); err != nil {
		t.Fatalf("repeated base recovery was not idempotent: %v", err)
	}
}

func TestRecoveryRefusesUnownedTargetDrift(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := startOperation(state, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plan.TargetPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plan.TargetPath, "external.txt"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	operation.Status = "partial"
	if err := writeOperation(state, operation); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanOperationRecovery(state, operation.ID, targetRoot); err == nil {
		t.Fatal("recovery plan accepted unowned target drift")
	}
	if got, err := os.ReadFile(filepath.Join(plan.TargetPath, "external.txt")); err != nil || string(got) != "preserve" {
		t.Fatalf("external file changed: %q %v", got, err)
	}
}

func TestRecoveryRejectsUnauthorizedTargetRoot(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := startOperation(state, plan)
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = "partial"
	if err := writeOperation(state, operation); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanOperationRecovery(state, operation.ID, t.TempDir()); err == nil {
		t.Fatal("recovery plan accepted a target root outside the authorized provider root")
	}
}

func TestRecoveryPlanRejectsTargetChangedAfterPreview(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := startOperation(state, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(plan.TargetPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(plan.SourceRoot, plan.TargetPath); err != nil {
		t.Fatal(err)
	}
	operation.Status = "partial"
	if err := writeOperation(state, operation); err != nil {
		t.Fatal(err)
	}
	recoveryPlan, err := PlanOperationRecovery(state, operation.ID, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plan.TargetPath, "SKILL.md"), []byte("external edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyOperationRecovery(recoveryPlan, targetRoot, state); err == nil {
		t.Fatal("recovery applied after target drift")
	}
	if got, err := os.ReadFile(filepath.Join(plan.TargetPath, "SKILL.md")); err != nil || string(got) != "external edit" {
		t.Fatalf("external edit was lost: %q %v", got, err)
	}
}

func TestDeploymentRemovalIsPlannedOwnedAndAllowsFreshRedeploy(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Apply(plan, libraryRoot, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	removePlan, err := PlanRemoveDeployment(first.ID, "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.TargetPath); err != nil {
		t.Fatalf("removal preview changed target: %v", err)
	}
	removed, err := ApplyRemoveDeployment(removePlan, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Status != "removed" || removed.LastOperationID != removePlan.PlanHash {
		t.Fatalf("removal record missing: %+v", removed)
	}
	if _, err := os.Lstat(first.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("managed target remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, filepath.FromSlash(removed.RemovalSnapshot))); err != nil {
		t.Fatalf("removal snapshot missing: %v", err)
	}
	if _, err := ApplyRemoveDeployment(removePlan, targetRoot, state); err != nil {
		t.Fatalf("same removal plan was not idempotent: %v", err)
	}
	redeployPlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if redeployPlan.TargetAction != "create" || redeployPlan.PreviousDeploymentID != first.ID {
		t.Fatalf("removed deployment was not treated as an archived generation: %+v", redeployPlan)
	}
	second, err := Apply(redeployPlan, libraryRoot, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.Status != "applied" {
		t.Fatalf("redeploy reused removed ownership record: %+v", second)
	}
}

func TestRemoveDeploymentRejectsTargetDrift(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := Apply(plan, libraryRoot, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	removePlan, err := PlanRemoveDeployment(deployed.ID, "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deployed.TargetPath, "SKILL.md"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRemoveDeployment(removePlan, targetRoot, state); err == nil {
		t.Fatal("expected drift to block deployment removal")
	}
	if got, err := os.ReadFile(filepath.Join(deployed.TargetPath, "SKILL.md")); err != nil || string(got) != "external" {
		t.Fatalf("external edit was lost: %q %v", got, err)
	}
}

func TestRemoveLatestDeploymentAfterUpdateCanRedeploy(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	firstPlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Apply(firstPlan, libraryRoot, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	updateSource := filepath.Join(t.TempDir(), "helper-v2")
	if err := os.Mkdir(updateSource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateSource, "SKILL.md"), []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Update(libraryRoot, state, "helper", updateSource, "skills"); err != nil {
		t.Fatal(err)
	}
	updatePlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Apply(updatePlan, libraryRoot, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	removePlan, err := PlanRemoveDeployment(updated.ID, "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRemoveDeployment(removePlan, targetRoot, state); err != nil {
		t.Fatal(err)
	}
	redeployPlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if redeployPlan.TargetAction != "create" || redeployPlan.PreviousDeploymentID != updated.ID {
		t.Fatalf("redeploy did not link to removed active generation (first=%s): %+v", first.ID, redeployPlan)
	}
	if _, err := Apply(redeployPlan, libraryRoot, targetRoot, state); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPreservesUnownedTarget(t *testing.T) {
	library, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(targetRoot, "helper"), 0700); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(targetRoot, "helper", "unknown.txt")
	if err := os.WriteFile(unknown, []byte("third-party"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSkill(library, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, library, targetRoot, state); err == nil {
		t.Fatal("expected unowned target to be rejected")
	}
	if got, err := os.ReadFile(unknown); err != nil || string(got) != "third-party" {
		t.Fatalf("unowned file changed: %q %v", got, err)
	}
}

func TestApplyRejectsTargetThatWasPresentWhenPlanWasMade(t *testing.T) {
	library, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	target := filepath.Join(targetRoot, "helper")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "external.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSkill(library, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.TargetExists {
		t.Fatal("plan should report the existing destination")
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, library, targetRoot, state); err == nil {
		t.Fatal("expected apply to reject a plan whose destination precondition was false")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("stale plan wrote a destination: %v", err)
	}
}

func TestApplyDoesNotReplaceEmptyTargetCreatedBeforeCommit(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	rename := func(oldPath, newPath string) error {
		if newPath == plan.TargetPath && strings.HasPrefix(filepath.Base(oldPath), ".agenthub-stage-") {
			if err := os.Mkdir(newPath, 0700); err != nil {
				return err
			}
		}
		return fsutil.RenameNoReplace(oldPath, newPath)
	}
	if _, err := applyWithRename(plan, libraryRoot, targetRoot, state, rename); err == nil {
		t.Fatal("apply replaced an empty destination created after the last precondition check")
	}
	entries, err := os.ReadDir(plan.TargetPath)
	if err != nil || len(entries) != 0 {
		t.Fatalf("concurrent destination was changed: entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(libraryRoot, "skills", "helper", "SKILL.md")); err != nil {
		t.Fatalf("canonical source was changed: %v", err)
	}
}

func TestApplyRejectsCanonicalChangeAfterPlan(t *testing.T) {
	library, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(library, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library, "skills", "helper", "SKILL.md"), []byte("changed after plan"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, library, targetRoot, state); err == nil {
		t.Fatal("expected apply to reject a source changed after planning")
	}
	if _, err := os.Lstat(filepath.Join(targetRoot, "helper")); !os.IsNotExist(err) {
		t.Fatalf("stale plan wrote a destination: %v", err)
	}
}

func TestApplyCleansNewProviderDirectoriesWhenStateWriteCannotStart(t *testing.T) {
	library, targetParent, stateParent := canonicalSkill(t), t.TempDir(), t.TempDir()
	state := t.TempDir()
	targetRoot := filepath.Join(targetParent, "new-provider", "skills")
	stateFile := filepath.Join(stateParent, "not-a-directory")
	if err := os.WriteFile(stateFile, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSkill(library, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, library, targetRoot, stateFile); err == nil {
		t.Fatal("expected state root failure")
	}
	if _, err := os.Lstat(filepath.Join(targetParent, "new-provider")); !os.IsNotExist(err) {
		t.Fatalf("failed apply left empty provider directories: %v", err)
	}
}

func TestPlanDoesNotAdoptSharedUnregisteredSkill(t *testing.T) {
	libraryRoot, targetRoot, state := t.TempDir(), t.TempDir(), t.TempDir()
	writeSkill(t, libraryRoot)
	if _, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state); err == nil {
		t.Fatal("expected shared skill without AgentHub ownership to remain a candidate")
	}
}

func TestPlanRejectsUnsafeCrossPlatformSlugs(t *testing.T) {
	for _, slug := range []string{"", "..", "../escape", "bad:name", "CON", "nul.txt", "trailing."} {
		if validSlug(slug) {
			t.Errorf("accepted unsafe slug %q", slug)
		}
	}
}

func TestOwnedUpdateSnapshotsDeployedVersionAndRollbackRestoresIt(t *testing.T) {
	libraryRoot, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	firstPlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Apply(firstPlan, libraryRoot, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	updatedSource := filepath.Join(t.TempDir(), "helper-v2")
	if err := os.Mkdir(updatedSource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updatedSource, "SKILL.md"), []byte("# helper v2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Update(libraryRoot, state, "helper", updatedSource, "skills"); err != nil {
		t.Fatal(err)
	}
	updatePlan, err := PlanSkill(libraryRoot, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if updatePlan.TargetAction != "update" || updatePlan.PreviousDeploymentID != first.ID {
		t.Fatalf("update plan did not establish existing ownership: %+v", updatePlan)
	}
	updated, err := Apply(updatePlan, libraryRoot, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := ListDeployments(state, "all")
	if err != nil || len(listed) != 2 || listed[0].Status != "superseded" && listed[1].Status != "superseded" {
		t.Fatalf("deployment list did not mark the previous revision superseded: %+v %v", listed, err)
	}
	if updated.PreviousHash != first.ContentHash || updated.Snapshot == "" {
		t.Fatalf("update did not record its rollback snapshot: %+v", updated)
	}
	if got, err := os.ReadFile(filepath.Join(targetRoot, "helper", "SKILL.md")); err != nil || string(got) != "# helper v2\n" {
		t.Fatalf("new deployed skill missing: %q %v", got, err)
	}
	if err := Rollback(updated.ID, "codex", targetRoot, state); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(targetRoot, "helper", "SKILL.md")); err != nil || string(got) != "# helper\n" {
		t.Fatalf("rollback did not restore the prior version: %q %v", got, err)
	}
}

func TestRollbackRefusesChangedDeployment(t *testing.T) {
	library, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(library, "helper", "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := Apply(plan, library, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(targetRoot, "helper", "external.txt")
	if err := os.WriteFile(changed, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(deployment.ID, "codex", targetRoot, state); err == nil {
		t.Fatal("expected rollback to reject post-deployment edits")
	}
	if got, err := os.ReadFile(changed); err != nil || string(got) != "keep me" {
		t.Fatalf("post-deployment edit was lost: %q %v", got, err)
	}
}

func TestRollbackRemovesOnlyUnchangedOwnedSkill(t *testing.T) {
	library, targetRoot, state := canonicalSkill(t), t.TempDir(), t.TempDir()
	plan, err := PlanSkill(library, "helper", "claude-code", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := Apply(plan, library, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := Rollback(deployment.ID, "claude-code", targetRoot, state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(targetRoot, "helper")); !os.IsNotExist(err) {
		t.Fatalf("owned target remains after rollback: %v", err)
	}
}
