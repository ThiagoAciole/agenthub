package discovery

import (
	"github.com/ThiagoAciole/agenthub/internal/models"
	"github.com/ThiagoAciole/agenthub/internal/providers"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanProjectAgentAndMCPNames(t *testing.T) {
	dir := t.TempDir()
	agent := filepath.Join(dir, ".claude", "agents", "helper.md")
	if err := os.MkdirAll(filepath.Dir(agent), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent, []byte("helper"), 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(config, []byte(`{"mcpServers":{"safe-name":{"token":"DO_NOT_PRINT"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	items, err := Scan([]models.Root{{Provider: "claude-code", Scope: "project", Path: filepath.Join(dir, ".claude", "agents")}, {Provider: "claude-code", Scope: "project", Path: config}}, providers.All())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	for _, item := range items {
		if item.Type == models.MCP && item.Name != "safe-name" {
			t.Fatalf("unexpected MCP name %q", item.Name)
		}
	}
	for _, item := range items {
		if strings.Contains(item.Name, "DO_NOT_PRINT") {
			t.Fatal("secret value was exposed")
		}
	}
}

func TestScanDistinguishesPromptsFromCommands(t *testing.T) {
	project := t.TempDir()
	prompts := filepath.Join(project, ".codex", "prompts")
	commands := filepath.Join(project, ".codex", "commands")
	for dir, file := range map[string]string{prompts: "review.md", commands: "review.md"} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	items, err := Scan([]models.Root{
		{Provider: "codex", Scope: "project", Path: prompts},
		{Provider: "codex", Scope: "project", Path: commands},
	}, providers.All())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]models.Type{}
	for _, item := range items {
		got[filepath.Base(filepath.Dir(item.Path))] = item.Type
	}
	if len(got) != 2 || got["prompts"] != models.Prompt || got["commands"] != models.Command {
		t.Fatalf("prompt/command kinds were conflated: %+v", items)
	}
}

func TestScanTreatsSkillDirectoryAsOneBundle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	skill := filepath.Join(root, "helper")
	for path, content := range map[string]string{
		filepath.Join(skill, "SKILL.md"):               "main",
		filepath.Join(skill, "README.md"):              "extra",
		filepath.Join(skill, "references", "guide.md"): "reference",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	items, err := Scan([]models.Root{{Provider: "claude-code", Scope: "project", Path: root}}, providers.All())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != models.Skill || items[0].Name != "helper" || items[0].Path != filepath.Join(skill, "SKILL.md") {
		t.Fatalf("skill bundle produced noncanonical candidates: %+v", items)
	}
}

func TestScanMissingRoot(t *testing.T) {
	items, err := Scan([]models.Root{{Provider: "codex", Scope: "global", Path: filepath.Join(t.TempDir(), "missing")}}, providers.All())
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestScanOpenCodeSharedSkillsUsesDirectoryName(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	for _, root := range []string{filepath.Join(home, ".agents", "skills"), filepath.Join(project, ".agents", "skills")} {
		skill := filepath.Join(root, "helper")
		if err := os.MkdirAll(skill, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: helper\ndescription: Fixture\n---\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	items, err := Scan((providers.OpenCode{}).Roots(home, project), providers.All())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d OpenCode skills; want global and project entries: %+v", len(items), items)
	}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Provider != "opencode" || item.Type != models.Skill || item.Name != "helper" {
			t.Errorf("unexpected shared skill inventory item: %+v", item)
		}
		seen[item.Origin] = true
	}
	if !seen["global"] || !seen["project"] {
		t.Errorf("shared skills missing scope(s): %+v", seen)
	}
}
