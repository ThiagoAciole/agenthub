package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThiagoAciole/agenthub/internal/deployment"
	"github.com/ThiagoAciole/agenthub/internal/library"
	"github.com/ThiagoAciole/agenthub/internal/models"
)

func TestProvidersJSONCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"providers", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var statuses []struct {
		ID               string `json:"id"`
		RuntimeDetection string `json:"runtime_detection"`
		SkillPublication string `json:"skill_publication"`
	}
	if err := json.Unmarshal(out.Bytes(), &statuses); err != nil {
		t.Fatal(err)
	}
	ids := []string{statuses[0].ID, statuses[1].ID, statuses[2].ID}
	if strings.Join(ids, ",") != "claude-code,codex,opencode" {
		t.Fatalf("unexpected providers: %v", ids)
	}
	for _, status := range statuses {
		if status.RuntimeDetection != "not-probed" || status.SkillPublication != "not-tested" {
			t.Fatalf("unverified provider capability claimed: %+v", status)
		}
	}
}
func TestInvalidScopeFails(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"scan", "--scope", "invalid"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected invalid scope error")
	}
}

func TestInitCreatesOnlyAgentHubConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	shared := filepath.Join(home, ".agents", "agents.lock.json")
	if err := os.MkdirAll(filepath.Dir(shared), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte("external-lock\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"init", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".agenthub", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config was not created: %v", err)
	}
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("init should be idempotent: %v", err)
	}
	if got, err := os.ReadFile(shared); err != nil || string(got) != "external-lock\n" {
		t.Fatalf("init changed shared .agents content: %q %v", got, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "{\n  \"version\": 1\n}\n" {
		t.Fatalf("init changed its existing config: %q %v", got, err)
	}
}

func TestScanProjectJSON(t *testing.T) {
	project := t.TempDir()
	dir := filepath.Join(project, ".codex", "agents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.md"), []byte("helper"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"scan", "--scope", "project", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Items []struct {
			Name   string `json:"name"`
			Origin string `json:"origin"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Name != "helper" || result.Items[0].Origin != "project" {
		t.Fatalf("unexpected scan result: %+v", result)
	}
}

func TestAddLocalSkillUsesProjectLibrary(t *testing.T) {
	project, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"add", source, "--type", "skills", "--scope", "project", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan library.AddPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatalf("add preview wrote to project library: %v", err)
	}
	planPath := filepath.Join(t.TempDir(), "add.json")
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"add", "--plan", planPath, "--yes", "--scope", "project", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", filepath.Base(source), "SKILL.md")); err != nil {
		t.Fatalf("approved add did not copy resource to project library: %v", err)
	}
}

func TestAddGitCLIPlansAndAppliesPinnedCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	for _, args := range [][]string{{"-C", repo, "init", "-b", "main"}, {"-C", repo, "config", "user.name", "AgentHub Test"}, {"-C", repo, "config", "user.email", "agenthub@example.invalid"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"), []byte("# pinned\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "add", "SKILL.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "commit", "-m", "fixture").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	bare := filepath.Join(t.TempDir(), "skill.git")
	if out, err := exec.Command("git", "clone", "--bare", repo, bare).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v: %s", err, out)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"add", bare, "--ref", "main", "--scope", "global", "--dry-run", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan library.AddPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.SourceKind != "git" || plan.GitCommit == "" || plan.Type != "skills" {
		t.Fatalf("CLI did not return pinned Git plan: %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatalf("Git plan wrote library: %v", err)
	}
	planPath := filepath.Join(t.TempDir(), "add-git.json")
	b, _ := json.Marshal(plan)
	if err := os.WriteFile(planPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"add", "--plan", planPath, "--yes", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var entry library.Entry
	if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Origin != "git" || entry.Source == nil || entry.Source.Commit != plan.GitCommit {
		t.Fatalf("Git source provenance missing: %+v", entry)
	}
}

func TestImportPreviewsWithoutWritingByDefault(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(project, ".claude", "skills", "helper")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "SKILL.md"), []byte("safe skill"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"import", "--from", "claude", "--scope", "project", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote to library: %v", err)
	}
	var plan importPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || len(plan.Candidates) != 1 || plan.Candidates[0].AddPlan == nil || plan.PlanHash == "" {
		t.Fatalf("preview did not produce a hash-bound candidate plan: %+v %v", plan, err)
	}
}

func TestListShowsCanonicalLibraryAndSeparatesProviderScan(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	for root, resource := range map[string]string{
		filepath.Join(home, ".agents"):    filepath.Join(t.TempDir(), "global-skill"),
		filepath.Join(project, ".agents"): filepath.Join(t.TempDir(), "project-skill"),
	} {
		if err := os.MkdirAll(resource, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(resource, "SKILL.md"), []byte(filepath.Base(resource)), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := library.Add(root, resource, "skills"); err != nil {
			t.Fatal(err)
		}
	}
	external := filepath.Join(project, ".claude", "skills", "external", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(external), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("provider only"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"list", "--scope", "all", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var listed []struct {
		Scope string `json:"scope"`
		Name  string `json:"name"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("list returned discovery inventory instead of the two canonical resources: %+v", listed)
	}
	seen := map[string]bool{}
	for _, entry := range listed {
		seen[entry.Scope+":"+entry.Name] = entry.Type == "skills"
	}
	if !seen["global:global-skill"] || !seen["project:project-skill"] || seen["project:external"] {
		t.Fatalf("canonical scopes or provider separation incorrect: %+v", listed)
	}
}

func TestShowReturnsCanonicalResourceByStableIDAcrossScopes(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	var globalEntry library.Entry
	for i, root := range []string{filepath.Join(home, ".agents"), filepath.Join(project, ".agents")} {
		source := filepath.Join(t.TempDir(), "demo")
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("canonical"), 0600); err != nil {
			t.Fatal(err)
		}
		entry, err := library.Add(root, source, "skills")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			globalEntry = entry
		}
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"show", globalEntry.ID, "--scope", "global", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var shown scopedLibraryEntry
	if err := json.Unmarshal(out.Bytes(), &shown); err != nil || shown.Scope != "global" || shown.ID != globalEntry.ID || shown.CurrentRevision.SHA256 != shown.SHA256 {
		t.Fatalf("show omitted canonical metadata: %+v %v", shown, err)
	}
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"show", globalEntry.ID, "--scope", "all", "--project", project})
	if err := root.Execute(); err != nil {
		t.Fatalf("unique resource ID did not resolve across scopes: %v", err)
	}
}

func TestRenameCommandRequiresReviewedPlanAndPreservesResourceID(t *testing.T) {
	home, project, source := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("canonical"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := library.Add(filepath.Join(home, ".agents"), source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"rename", entry.Name, "--type", "skills", "--to", "new-name", "--scope", "global", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan library.RenamePlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(t.TempDir(), "rename-plan.json")
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"rename", entry.Name, "--plan", planPath, "--yes", "--scope", "global", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var renamed library.Entry
	if err := json.Unmarshal(out.Bytes(), &renamed); err != nil || renamed.ID != entry.ID || renamed.Name != "new-name" {
		t.Fatalf("rename command did not preserve identity: %+v %v", renamed, err)
	}
}

func TestFindSearchesCanonicalMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	source := filepath.Join(t.TempDir(), "alpha")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("needle-in-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Add(filepath.Join(home, ".agents"), source, "skills"); err != nil {
		t.Fatal(err)
	}
	knowledge := filepath.Join(t.TempDir(), "private-notes")
	if err := os.MkdirAll(filepath.Join(knowledge, "references"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(knowledge, "references", "guide.md"), []byte("Unique knowledge phrase for local search"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Add(filepath.Join(home, ".agents"), knowledge, "knowledge"); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"find", "ALPHA", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var found []scopedLibraryEntry
	if err := json.Unmarshal(out.Bytes(), &found); err != nil || len(found) != 1 || found[0].Name != "alpha" {
		t.Fatalf("find did not match canonical metadata case-insensitively: %+v %v", found, err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"find", "needle-in-content", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	found = nil
	if err := json.Unmarshal(out.Bytes(), &found); err != nil || len(found) != 0 {
		t.Fatalf("metadata search unexpectedly exposed content: %+v %v", found, err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"find", "UNIQUE KNOWLEDGE PHRASE", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	found = nil
	if err := json.Unmarshal(out.Bytes(), &found); err != nil || len(found) != 1 || found[0].Type != "knowledge" || found[0].Name != "private-notes" {
		t.Fatalf("find did not return matching knowledge resource: %+v %v", found, err)
	}
	if strings.Contains(out.String(), "Unique knowledge phrase") {
		t.Fatal("find output exposed document content")
	}
}

func TestImportAppliesOnlySelectedCandidateFromUnchangedPlan(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	for slug, body := range map[string]string{"alpha": "alpha skill", "beta": "beta skill"} {
		skill := filepath.Join(project, ".claude", "skills", slug)
		if err := os.MkdirAll(skill, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if slug == "beta" {
			if err := os.MkdirAll(filepath.Join(skill, "references"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(skill, "README.md"), []byte("auxiliary"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(skill, "references", "guide.md"), []byte("reference"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"import", "--from", "claude", "--scope", "project", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan importPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || len(plan.Candidates) != 2 {
		t.Fatalf("unexpected import preview: %+v %v", plan, err)
	}
	planPath := filepath.Join(t.TempDir(), "import.json")
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	selectedID := ""
	for _, candidate := range plan.Candidates {
		if candidate.Candidate.Slug == "beta" {
			selectedID = candidate.ID
		}
	}
	if selectedID == "" {
		t.Fatal("beta candidate not found in preview")
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"import", "--from", "claude", "--scope", "project", "--project", project, "--plan", planPath, "--select", selectedID, "--yes", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result importResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != "applied" || len(result.Applied) != 1 || result.Applied[0].Name != "beta" {
		t.Fatalf("import did not apply exactly the selected candidate: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("unselected candidate was imported: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(project, ".agents", "skills", "beta", "SKILL.md")); err != nil || string(got) != "beta skill" {
		t.Fatalf("selected candidate missing from canonical library: %q %v", got, err)
	}
	for path, want := range map[string]string{
		filepath.Join("README.md"):              "auxiliary",
		filepath.Join("references", "guide.md"): "reference",
	} {
		got, err := os.ReadFile(filepath.Join(project, ".agents", "skills", "beta", path))
		if err != nil || string(got) != want {
			t.Fatalf("skill bundle file %q missing from canonical library: %q %v", path, got, err)
		}
	}
}

func TestImportRejectsChangedSourceAfterPreview(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	skill := filepath.Join(project, ".claude", "skills", "helper")
	if err := os.MkdirAll(skill, 0700); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(skill, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"import", "--from", "claude", "--scope", "project", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan importPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || len(plan.Candidates) != 1 {
		t.Fatalf("unexpected import preview: %+v %v", plan, err)
	}
	planPath := filepath.Join(t.TempDir(), "import.json")
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"import", "--from", "claude", "--scope", "project", "--project", project, "--plan", planPath, "--select", "1", "--yes", "--json"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "mudou após a prévia") {
		t.Fatalf("changed source was imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "helper")); !os.IsNotExist(err) {
		t.Fatalf("stale plan wrote to library: %v", err)
	}
}

func TestImportCodexPromptPreservesPromptKindAndContents(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	promptPath := filepath.Join(project, ".codex", "prompts", "review.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("Review only the changed files.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"import", "--from", "codex", "--scope", "project", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan importPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || len(plan.Candidates) != 1 {
		t.Fatalf("unexpected prompt import preview: %+v %v", plan, err)
	}
	candidate := plan.Candidates[0]
	if candidate.Candidate.Kind != models.Prompt || candidate.AddPlan == nil || candidate.AddPlan.Type != "prompts" || candidate.AddPlan.Name != "review" {
		t.Fatalf("prompt was misclassified or misnamed: %+v", candidate)
	}
	planPath := filepath.Join(t.TempDir(), "import.json")
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"import", "--from", "codex", "--scope", "project", "--project", project, "--plan", planPath, "--select", candidate.ID, "--yes", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result importResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != "applied" || len(result.Applied) != 1 || result.Applied[0].Type != "prompts" {
		t.Fatalf("prompt was not imported as a prompt: %+v %v", result, err)
	}
	canonical := filepath.Join(project, ".agents", "prompts", "review", "review.md")
	if got, err := os.ReadFile(canonical); err != nil || string(got) != "Review only the changed files.\n" {
		t.Fatalf("prompt content was not copied intact: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "commands", "review")); !os.IsNotExist(err) {
		t.Fatalf("prompt was written to commands: %v", err)
	}
}

func TestMigrateDryRunCLIUsesExplicitRootsWithoutWriting(t *testing.T) {
	home, source, target := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "canonical")
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	skill := filepath.Join(source, "skills", "demo")
	if err := os.MkdirAll(skill, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"migrate", "--dry-run", "--source", source, "--destination", target, "--legacy-lock", filepath.Join(source, "agenthub.lock.json")})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan library.MigrationPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || len(plan.Items) != 1 || plan.Items[0].Status != "ready" {
		t.Fatalf("unexpected migrate preview: %+v %v", plan, err)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "demo")); !os.IsNotExist(err) {
		t.Fatalf("migrate dry-run wrote destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agenthub")); !os.IsNotExist(err) {
		t.Fatalf("migrate dry-run wrote private state: %v", err)
	}
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"migrate", "--source", source, "--destination", target})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "somente --dry-run") {
		t.Fatalf("migrate without dry-run was not rejected: %v", err)
	}
}

func TestSkillPlanApplyRollbackUsesIsolatedProviderRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	libraryRoot := filepath.Join(home, ".agents")
	source := filepath.Join(t.TempDir(), "fixture")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("# fixture skill\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "assets", "note.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Add(libraryRoot, source, "skills"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".codex", "skills", "fixture")
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"plan", "fixture", "--provider", "codex", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan deployment.SkillPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.TargetPath != target {
		t.Fatalf("plan target = %q; want %q", plan.TargetPath, target)
	}
	if plan.TargetAction != "create" || len(plan.FilesToCreate) != 2 || plan.FilesToCreate[0] != "SKILL.md" || plan.FilesToCreate[1] != "assets/note.txt" {
		t.Fatalf("plan did not enumerate the skill bundle: %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(home, ".agenthub")); !os.IsNotExist(err) {
		t.Fatalf("plan wrote private state: %v", err)
	}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planBytes, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"apply", "--plan", planPath, "--yes", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var applied deployment.Deployment
	if err := json.Unmarshal(out.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "assets", "note.txt")); err != nil || string(got) != "data" {
		t.Fatalf("auxiliary file was not deployed: %q %v", got, err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"rollback", applied.ID, "--provider", "codex", "--scope", "global", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("rollback left the fixture deployment: %v", err)
	}
}

func TestApplyRequiresExplicitApproval(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"apply", "--plan", "missing.json"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("apply should require approval before reading a plan: %v", err)
	}
}

func TestUpdateCommandPreviewsThenAppliesWithSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	rootPath := filepath.Join(home, ".agents")
	sourceRoot := filepath.Join(t.TempDir(), "helper")
	if err := os.Mkdir(sourceRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := library.Add(rootPath, sourceRoot, "skills")
	if err != nil {
		t.Fatal(err)
	}
	updateSource := filepath.Join(t.TempDir(), "helper-next")
	if err := os.Mkdir(updateSource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateSource, "SKILL.md"), []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"update", entry.Name, updateSource, "--type", "skills", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan library.UpdatePlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(rootPath, "skills", entry.Name, "SKILL.md")
	if got, err := os.ReadFile(canonical); err != nil || string(got) != "v1" {
		t.Fatalf("preview changed canonical data: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agenthub")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote private state: %v", err)
	}
	planPath := filepath.Join(t.TempDir(), "update.json")
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planBytes, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"update", "--plan", planPath, "--yes", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	updated, err := library.List(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].ID != entry.ID || len(updated[0].Revisions) != 1 {
		t.Fatalf("updated registry lost history or identity: %+v", updated)
	}
	if got, err := os.ReadFile(canonical); err != nil || string(got) != "v2" {
		t.Fatalf("approved update missing: %q %v", got, err)
	}
}

func TestRemoveAndRestoreCommandsUsePrivateTrashPlans(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	rootPath := filepath.Join(home, ".agents")
	source := filepath.Join(t.TempDir(), "recoverable")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := library.Add(rootPath, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	providerSkill := filepath.Join(home, ".codex", "skills", entry.Name)
	if err := os.MkdirAll(providerSkill, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providerSkill, "SKILL.md"), []byte("provider copy"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"remove", entry.Name, "--type", "skills", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var removePlan library.RemovePlan
	if err := json.Unmarshal(out.Bytes(), &removePlan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, "skills", entry.Name)); err != nil {
		t.Fatalf("remove preview changed canonical library: %v", err)
	}
	planPath := filepath.Join(t.TempDir(), "remove.json")
	b, _ := json.Marshal(removePlan)
	if err := os.WriteFile(planPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"remove", "--plan", planPath, "--scope", "global", "--yes", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, "skills", entry.Name)); !os.IsNotExist(err) {
		t.Fatalf("approved remove kept canonical resource: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(providerSkill, "SKILL.md")); err != nil || string(got) != "provider copy" {
		t.Fatalf("remove altered provider deployment: %q %v", got, err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"restore", entry.Name, "--type", "skills", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var restorePlan library.RestorePlan
	if err := json.Unmarshal(out.Bytes(), &restorePlan); err != nil {
		t.Fatal(err)
	}
	restorePath := filepath.Join(t.TempDir(), "restore.json")
	b, _ = json.Marshal(restorePlan)
	if err := os.WriteFile(restorePath, b, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"restore", "--plan", restorePath, "--scope", "global", "--yes", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(rootPath, "skills", entry.Name, "SKILL.md")); err != nil || string(got) != "safe" {
		t.Fatalf("restore failed: %q %v", got, err)
	}
}

func TestHistoryCommandShowsCurrentAndPreviousRevision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	rootPath := filepath.Join(home, ".agents")
	source := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(source, []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := library.Add(rootPath, source, "prompts")
	if err != nil {
		t.Fatal(err)
	}
	updateSource := filepath.Join(t.TempDir(), "prompt-v2.md")
	if err := os.WriteFile(updateSource, []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Update(rootPath, filepath.Join(home, ".agenthub"), entry.Name, updateSource, "prompts"); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"history", entry.Name, "--type", "prompts", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var history library.History
	if err := json.Unmarshal(out.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Revisions) != 2 || history.Revisions[0].Current || !history.Revisions[1].Current || history.Revisions[0].SHA256 == history.Revisions[1].SHA256 {
		t.Fatalf("unexpected CLI history: %+v", history)
	}
}

func TestDeploymentsListAndSeparateRemovalPlan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	libraryRoot, state, targetRoot := filepath.Join(home, ".agents"), filepath.Join(home, ".agenthub"), filepath.Join(home, ".codex", "skills")
	source := filepath.Join(t.TempDir(), "helper")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("managed"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := library.Add(libraryRoot, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := deployment.PlanSkill(libraryRoot, entry.Name, "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := deployment.Apply(plan, libraryRoot, targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"deployments", "--scope", "all", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var listed []deployment.Deployment
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != deployed.ID {
		t.Fatalf("unexpected deployment list: %+v %v", listed, err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"deployments", "remove", deployed.ID, "--provider", "codex", "--scope", "global", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var removalPlan deployment.DeploymentRemovalPlan
	if err := json.Unmarshal(out.Bytes(), &removalPlan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(deployed.TargetPath); err != nil {
		t.Fatalf("deployment removal preview changed target: %v", err)
	}
	planPath := filepath.Join(t.TempDir(), "deployment-remove.json")
	b, _ := json.Marshal(removalPlan)
	if err := os.WriteFile(planPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"deployments", "remove", "--provider", "codex", "--scope", "global", "--plan", planPath, "--yes", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(deployed.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("approved deployment removal left target: %v", err)
	}
	var removed deployment.Deployment
	if err := json.Unmarshal(out.Bytes(), &removed); err != nil || removed.Status != "removed" {
		t.Fatalf("deployment removal did not retain status: %+v %v", removed, err)
	}
}

func TestOperationRecoveryCommandRequiresReviewedPlan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	libraryRoot, state, targetRoot := filepath.Join(home, ".agents"), filepath.Join(home, ".agenthub"), filepath.Join(home, ".codex", "skills")
	source := filepath.Join(t.TempDir(), "helper")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("managed"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := library.Add(libraryRoot, source, "skills")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := deployment.PlanSkill(libraryRoot, entry.Name, "codex", "global", targetRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deployment.Apply(plan, libraryRoot, targetRoot, state); err != nil {
		t.Fatal(err)
	}
	operation, err := deployment.ReadOperation(state, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(state, "deployments", plan.PlanHash, "deployment.json")); err != nil {
		t.Fatal(err)
	}
	operation.Status = "partial"
	b, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "operations", plan.PlanHash+".json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"operations", "recover", plan.PlanHash, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var recoveryPlan deployment.OperationRecoveryPlan
	if err := json.Unmarshal(out.Bytes(), &recoveryPlan); err != nil || recoveryPlan.Action != "finalize" {
		t.Fatalf("unexpected recovery preview: %+v %v", recoveryPlan, err)
	}
	planPath := filepath.Join(t.TempDir(), "recovery.json")
	if err := os.WriteFile(planPath, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"operations", "recover", plan.PlanHash, "--plan", planPath, "--json"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("recovery must require approval: %v", err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"operations", "recover", plan.PlanHash, "--plan", planPath, "--yes", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	operation, err = deployment.ReadOperation(state, plan.PlanHash)
	if err != nil || operation.Status != "applied" {
		t.Fatalf("approved recovery failed: %+v %v", operation, err)
	}
	out.Reset()
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"operations", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var operations []deployment.Operation
	if err := json.Unmarshal(out.Bytes(), &operations); err != nil || len(operations) != 1 || operations[0].Status != "applied" {
		t.Fatalf("operation journal was not listed: %+v %v", operations, err)
	}
}
