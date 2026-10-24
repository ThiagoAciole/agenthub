package providers

import (
	"github.com/ThiagoAciole/agenthub/internal/models"
	"path/filepath"
)

type Claude struct{}

func (Claude) Name() string { return "claude-code" }
func (Claude) Roots(g, p string) []models.Root {
	base := filepath.Join(g, ".claude")
	r := roots("claude-code", base, p, []string{"agents", "skills", "commands", "rules"}, ".claude")
	return append(r, models.Root{Provider: "claude-code", Scope: "global", Path: filepath.Join(base, "settings.json")}, models.Root{Provider: "claude-code", Scope: "project", Path: filepath.Join(p, ".mcp.json")})
}
