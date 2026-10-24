package providers

import (
	"github.com/ThiagoAciole/agenthub/internal/models"
	"os"
	"path/filepath"
)

type OpenCode struct{}

func (OpenCode) Name() string { return "opencode" }
func (OpenCode) Roots(g, p string) []models.Root {
	config, _ := os.UserConfigDir()
	base := filepath.Join(config, "opencode")
	r := roots("opencode", base, p, []string{"agents", "skills", "commands"}, ".opencode")
	return append(r, models.Root{Provider: "opencode", Scope: "global", Path: filepath.Join(base, "opencode.json")}, models.Root{Provider: "opencode", Scope: "project", Path: filepath.Join(p, "opencode.json")})
}
