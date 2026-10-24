package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvidersJSONCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"providers", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := json.Unmarshal(out.Bytes(), &names); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "claude-code,codex,opencode" {
		t.Fatalf("unexpected providers: %v", names)
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
	configRoot := t.TempDir()
	t.Setenv("APPDATA", configRoot)
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"init", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configRoot, "agenthub", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config was not created: %v", err)
	}
	root = NewRootCommand(&out, &errOut)
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected existing config to be preserved")
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
