package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanMigrationReportsHashesConflictsAndLegacyMetadataWithoutWriting(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	ready := filepath.Join(source, "skills", "ready")
	conflict := filepath.Join(source, "skills", "conflict")
	for _, path := range []string{ready, conflict, filepath.Join(target, "skills", "conflict")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ready, "SKILL.md"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflict, "SKILL.md"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "skills", "conflict", "SKILL.md"), []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	legacyLock := filepath.Join(target, "agenthub.lock.json")
	if err := os.WriteFile(legacyLock, []byte("legacy metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanMigration(source, target, legacyLock)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.SourcePresent || plan.PlanHash == "" || len(plan.Items) != 2 || plan.Metadata == nil {
		t.Fatalf("incomplete migration preview: %+v", plan)
	}
	if plan.Items[0].Name != "conflict" || plan.Items[0].Status != "conflict" || plan.Items[0].TargetHash == "" {
		t.Fatalf("destination conflict was not reported: %+v", plan.Items[0])
	}
	if plan.Items[1].Name != "ready" || plan.Items[1].Status != "ready" || plan.Items[1].SourceHash == "" {
		t.Fatalf("ready candidate hash missing: %+v", plan.Items[1])
	}
	if plan.Metadata.Status != "manual_review" || plan.Metadata.SourceHash == "" || plan.Metadata.Target != filepath.Join(target, ".agenthub", "registry.json") {
		t.Fatalf("legacy metadata was not isolated for manual review: %+v", plan.Metadata)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "ready")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote candidate to destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".agenthub")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created metadata destination: %v", err)
	}
	if got, err := os.ReadFile(legacyLock); err != nil || string(got) != "legacy metadata" {
		t.Fatalf("dry-run changed legacy lock bytes: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(ready, "SKILL.md")); err != nil || string(got) != "ready" {
		t.Fatalf("dry-run changed source: %q %v", got, err)
	}
}

func TestPlanMigrationMissingSourceIsReadOnly(t *testing.T) {
	base := t.TempDir()
	source, target := filepath.Join(base, "missing-library"), filepath.Join(base, "canonical")
	plan, err := PlanMigration(source, target, filepath.Join(base, "missing-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.SourcePresent || len(plan.Items) != 0 || plan.PlanHash == "" {
		t.Fatalf("missing legacy source was not represented safely: %+v", plan)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("dry-run created target root: %v", err)
	}
}

func TestPlanMigrationTreatsRegisteredResourceWithoutDirectoryAsConflict(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	resource := filepath.Join(source, "skills", "helper")
	if err := os.MkdirAll(resource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resource, "SKILL.md"), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeEntries(target, []Entry{{ID: "skills:helper", Name: "helper", Type: "skills", SHA256: "registered"}}); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanMigration(source, target, filepath.Join(source, "missing-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Status != "conflict" || !strings.Contains(plan.Items[0].Reason, "registrado") {
		t.Fatalf("registered resource without content was considered safe to migrate: %+v", plan.Items)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "helper")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created the missing registered resource: %v", err)
	}
}

func TestPlanMigrationDetectsCaseInsensitiveDestinationCollision(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	resource := filepath.Join(source, "skills", "helper")
	if err := os.MkdirAll(resource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resource, "SKILL.md"), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(target, "skills", "HELPER")
	if err := os.MkdirAll(existing, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "SKILL.md"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanMigration(source, target, filepath.Join(source, "missing-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Status != "conflict" {
		t.Fatalf("case-insensitive collision was not protected: %+v", plan.Items)
	}
	if got, err := os.ReadFile(filepath.Join(existing, "SKILL.md")); err != nil || string(got) != "external" {
		t.Fatalf("migration preview changed existing content: %q %v", got, err)
	}
}

func TestPlanMigrationBlocksNonPortableResourceName(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	resource := filepath.Join(source, "skills", "CON")
	if err := os.MkdirAll(resource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resource, "SKILL.md"), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanMigration(source, target, filepath.Join(source, "missing-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Status != "blocked" || !strings.Contains(plan.Items[0].Reason, "não é portátil") {
		t.Fatalf("migration preview accepted a Windows-reserved resource name: %+v", plan.Items)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "CON")); !os.IsNotExist(err) {
		t.Fatalf("migration preview wrote target: %v", err)
	}
}

func TestPlanMigrationRejectsRegistrySymlink(t *testing.T) {
	source, target, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "registry.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(target, ".agenthub")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := PlanMigration(source, target, filepath.Join(source, "missing-lock.json")); err == nil {
		t.Fatal("migration followed registry namespace symlink")
	}
}

func TestPlanMigrationRejectsOverlappingRoots(t *testing.T) {
	root := t.TempDir()
	if _, err := PlanMigration(root, filepath.Join(root, "canonical"), filepath.Join(root, "legacy-lock.json")); err == nil {
		t.Fatal("migration accepted overlapping source and destination")
	}
}
