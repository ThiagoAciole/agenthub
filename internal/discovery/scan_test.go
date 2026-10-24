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

func TestScanMissingRoot(t *testing.T) {
	items, err := Scan([]models.Root{{Provider: "codex", Scope: "global", Path: filepath.Join(t.TempDir(), "missing")}}, providers.All())
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}
