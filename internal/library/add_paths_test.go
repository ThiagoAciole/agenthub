package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanAddRejectsPortableNameViolations(t *testing.T) {
	root, source := t.TempDir(), filepath.Join(t.TempDir(), "helper.md")
	if err := os.WriteFile(source, []byte("helper"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CON", "NUL.txt", "NUL .txt", "CONIN$", "CONOUT$", "bad:name", "bad?name", ".git"} {
		if _, err := PlanAddLocal(root, source, "skills", name); err == nil {
			t.Errorf("accepted non-portable resource name %q", name)
		}
	}
}

func TestPlanAddRejectsCaseInsensitiveDirectoryCollision(t *testing.T) {
	root, source := t.TempDir(), filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(root, "skills", "Helper")
	if err := os.MkdirAll(existing, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "SKILL.md"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanAddLocal(root, source, "skills", "helper"); err == nil {
		t.Fatal("case-insensitive destination collision was accepted")
	}
	if got, err := os.ReadFile(filepath.Join(existing, "SKILL.md")); err != nil || string(got) != "external" {
		t.Fatalf("existing external resource changed: %q %v", got, err)
	}
}

func TestPlanAddRejectsCaseInsensitiveRegistryCollision(t *testing.T) {
	root, source := t.TempDir(), filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := appendLock(root, Entry{ID: "skills:Helper", Type: "skills", Name: "Helper", Status: "tombstoned"}); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanAddLocal(root, source, "skills", "helper"); err == nil {
		t.Fatal("case-insensitive registry collision was accepted")
	}
}

func TestCaseInsensitiveNameMatchingIsPortableAcrossFilesystems(t *testing.T) {
	if !hasCaseInsensitiveName([]string{"Helper", "Äktion"}, "helper") || !hasCaseInsensitiveName([]string{"Helper", "Äktion"}, "äktion") {
		t.Fatal("Unicode case variants were not recognized as collisions")
	}
}

func TestAddRoundTripsUnicodeAndSpacePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "biblioteca com espaço ç")
	source := filepath.Join(t.TempDir(), "skill ação")
	if err := os.MkdirAll(filepath.Join(source, "references"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(source, "SKILL.md"):                   "principal",
		filepath.Join(source, "references", "guia ação.md"): "referência",
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := PlanAddLocal(root, source, "skills", "")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ApplyAdd(context.Background(), plan, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "skills", entry.Name, "references", "guia ação.md"))
	if err != nil || string(got) != "referência" {
		t.Fatalf("Unicode resource path did not round-trip: %q %v", got, err)
	}
}
