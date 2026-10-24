package platform

import (
	"os"
	"path/filepath"
)

type Resolver struct{}

type Paths struct {
	Home           string
	Library        string
	State          string
	Project        string
	ProjectLibrary string
}

func New() Resolver { return Resolver{} }

// Resolve centralizes the canonical library and private application state roots.
func (Resolver) Resolve(project string) (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	paths := Paths{Home: home, Library: filepath.Join(home, ".agents"), State: filepath.Join(home, ".agenthub")}
	if project != "" {
		paths.Project, err = filepath.Abs(project)
		if err != nil {
			return Paths{}, err
		}
		paths.ProjectLibrary = filepath.Join(paths.Project, ".agents")
	}
	return paths, nil
}

func (Resolver) Roots(project string) (string, string, error) {
	paths, err := (Resolver{}).Resolve(project)
	if err != nil {
		return "", "", err
	}
	return paths.Home, paths.Project, nil
}
