package providers

import (
	"github.com/ThiagoAciole/agenthub/internal/models"
	"os"
	"path/filepath"
)

type Codex struct{}

func (Codex) Name() string { return "codex" }
func (Codex) Roots(g, p string) []models.Root {
	if x := os.Getenv("CODEX_HOME"); x != "" {
		g = x
	} else {
		g = filepath.Join(g, ".codex")
	}
	r := roots("codex", g, p, []string{"agents", "skills", "prompts"}, ".codex")
	return append(r, models.Root{Provider: "codex", Scope: "global", Path: filepath.Join(g, "config.toml")}, models.Root{Provider: "codex", Scope: "project", Path: filepath.Join(p, ".codex", "config.toml")}, models.Root{Provider: "codex", Scope: "project", Path: filepath.Join(p, "AGENTS.md")})
}
func roots(name, g, p string, dirs []string, projectDir string) []models.Root {
	r := []models.Root{}
	for _, d := range dirs {
		r = append(r, models.Root{Provider: name, Scope: "global", Path: filepath.Join(g, d)})
	}
	for _, d := range dirs {
		r = append(r, models.Root{Provider: name, Scope: "project", Path: filepath.Join(p, projectDir, d)})
	}
	return r
}
