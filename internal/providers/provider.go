package providers

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ThiagoAciole/agenthub/internal/models"
)

type Provider interface {
	Name() string
	Roots(global, project string) []models.Root
}

type Status struct {
	ID               string `json:"id"`
	AdapterStatus    string `json:"adapter_status"`
	RuntimeDetection string `json:"runtime_detection"`
	SkillPublication string `json:"skill_publication"`
	OtherPublication string `json:"other_publication"`
}

func All() []Provider { return []Provider{Codex{}, Claude{}, OpenCode{}} }
func Names() []string { return []string{"claude-code", "codex", "opencode"} }

func Describe() []Status {
	statuses := make([]Status, 0, len(Names()))
	for _, name := range Names() {
		statuses = append(statuses, Status{ID: name, AdapterStatus: "registered", RuntimeDetection: "not-probed", SkillPublication: "not-tested", OtherPublication: "not-tested"})
	}
	return statuses
}

// SkillRoot returns the declared candidate path for a provider skill adapter.
// The path declaration alone does not establish runtime support.
func SkillRoot(name, home, project, scope string) (string, error) {
	if scope != "global" && scope != "project" {
		return "", fmt.Errorf("escopo inválido: %s", scope)
	}
	var base string
	if scope == "project" {
		if project == "" {
			return "", fmt.Errorf("diretório do projeto obrigatório")
		}
		switch name {
		case "codex":
			base = filepath.Join(project, ".codex")
		case "claude-code":
			base = filepath.Join(project, ".claude")
		case "opencode":
			base = filepath.Join(project, ".opencode")
		default:
			return "", fmt.Errorf("provider não reconhecido: %s", name)
		}
	} else {
		switch name {
		case "codex":
			base = os.Getenv("CODEX_HOME")
			if base == "" {
				base = filepath.Join(home, ".codex")
			}
		case "claude-code":
			base = filepath.Join(home, ".claude")
		case "opencode":
			config, err := os.UserConfigDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(config, "opencode")
		default:
			return "", fmt.Errorf("provider não reconhecido: %s", name)
		}
	}
	return filepath.Join(base, "skills"), nil
}
