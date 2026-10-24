package models

type Type string

const (
	Agent   Type = "agent"
	Skill   Type = "skill"
	Command Type = "command"
	Rule    Type = "rule"
	MCP     Type = "mcp"
)

type Root struct {
	Provider string
	Scope    string
	Path     string
}
type Item struct {
	Provider   string   `json:"provider"`
	Type       Type     `json:"type"`
	Name       string   `json:"name"`
	Origin     string   `json:"origin"`
	Path       string   `json:"path"`
	Duplicates []string `json:"duplicates,omitempty"`
}
type DuplicateGroup struct {
	Hash  string   `json:"hash"`
	Paths []string `json:"paths"`
}
type Inventory struct {
	Items           []Item           `json:"items"`
	DuplicateGroups []DuplicateGroup `json:"duplicate_groups,omitempty"`
}
