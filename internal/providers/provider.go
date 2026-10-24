package providers

import "github.com/ThiagoAciole/agenthub/internal/models"

type Provider interface {
	Name() string
	Roots(global, project string) []models.Root
}

func All() []Provider { return []Provider{Codex{}, Claude{}, OpenCode{}} }
func Names() []string { return []string{"claude-code", "codex", "opencode"} }
