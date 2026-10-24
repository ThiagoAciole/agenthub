package providers

import (
	"github.com/ThiagoAciole/agenthub/internal/models"
	"os"
	"path/filepath"
)

type Claude struct{}

func (Claude) Name() string { return "claude-code" }
func (Claude) Roots(g, p string) []models.Root {
	home, _ := os.UserHomeDir()
	r := roots("claude-code", filepath.Join(home, ".claude"), p, []string{"agents", "skills", "commands", "rules"}, ".claude")
	return append(r, models.Root{Provider: "claude-code", Scope: "global", Path: filepath.Join(home, ".claude", "settings.json")}, models.Root{Provider: "claude-code", Scope: "project", Path: filepath.Join(p, ".mcp.json")})
}
