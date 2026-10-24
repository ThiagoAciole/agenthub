package models

type Type string

const (
	Agent       Type = "agent"
	Skill       Type = "skill"
	Prompt      Type = "prompt"
	Command     Type = "command"
	Rule        Type = "rule"
	Memory      Type = "memory"
	MCP         Type = "mcp"
	Plugin      Type = "plugin"
	Knowledge   Type = "knowledge"
	ProfileKind Type = "profile"
)

// Resource is the portable catalog identity. Revisions and deployments are
// represented separately so catalog data does not imply provider ownership.
type Resource struct {
	ID             string `json:"resource_id"`
	Kind           Type   `json:"kind"`
	Slug           string `json:"slug"`
	Scope          string `json:"scope"`
	Origin         string `json:"origin,omitempty"`
	ContentHash    string `json:"content_hash,omitempty"`
	BaseRevisionID string `json:"base_revision_id,omitempty"`
}

type Revision struct {
	ID             string `json:"revision_id"`
	ResourceID     string `json:"resource_id"`
	BaseRevisionID string `json:"base_revision_id,omitempty"`
	ContentHash    string `json:"content_hash"`
	Origin         string `json:"origin,omitempty"`
}

type ImportCandidate struct {
	SourcePath  string `json:"source_path"`
	Provider    string `json:"provider,omitempty"`
	Kind        Type   `json:"kind"`
	Slug        string `json:"slug"`
	ContentHash string `json:"content_hash,omitempty"`
}

type Deployment struct {
	ID            string `json:"deployment_id"`
	ResourceID    string `json:"resource_id"`
	Provider      string `json:"provider_id"`
	Scope         string `json:"scope"`
	TargetPath    string `json:"target_path"`
	InstalledHash string `json:"installed_hash,omitempty"`
	Status        string `json:"status"`
}

type Conflict struct {
	ResourceID string `json:"resource_id"`
	BaseHash   string `json:"base_hash,omitempty"`
	LocalHash  string `json:"local_hash,omitempty"`
	RemoteHash string `json:"remote_hash,omitempty"`
	Status     string `json:"status"`
}

type Profile struct {
	ID        string   `json:"profile_id"`
	Scope     string   `json:"scope"`
	Resources []string `json:"resources,omitempty"`
	Providers []string `json:"providers,omitempty"`
}

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
