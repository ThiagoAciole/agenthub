package providers

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDescribeDoesNotClaimRuntimeSupport(t *testing.T) {
	for _, provider := range Describe() {
		if provider.AdapterStatus != "registered" || provider.RuntimeDetection != "not-probed" || provider.SkillPublication != "not-tested" {
			t.Fatalf("provider claims unverified support: %+v", provider)
		}
	}
}

func TestSkillRootsUseDeclaredProviderLayouts(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	for _, test := range []struct {
		provider, scope, want string
	}{
		{"codex", "global", filepath.Join(home, ".codex", "skills")},
		{"claude-code", "global", filepath.Join(home, ".claude", "skills")},
		{"claude-code", "project", filepath.Join(project, ".claude", "skills")},
		{"opencode", "project", filepath.Join(project, ".opencode", "skills")},
	} {
		got, err := SkillRoot(test.provider, home, project, test.scope)
		if err != nil || got != test.want {
			t.Errorf("SkillRoot(%s, %s) = %q, %v; want %q", test.provider, test.scope, got, err, test.want)
		}
	}
}

func TestClaudeRootsUseInjectedHome(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	globalRoots := 0
	for _, root := range (Claude{}).Roots(home, project) {
		if root.Scope != "global" {
			continue
		}
		globalRoots++
		relative, err := filepath.Rel(home, root.Path)
		if err != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Errorf("global Claude root escaped injected home: %q (%q, %v)", root.Path, relative, err)
		}
	}
	if globalRoots != 5 {
		t.Fatalf("got %d global Claude roots, want 5", globalRoots)
	}
}
