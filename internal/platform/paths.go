package platform

import "os"

type Resolver struct{}

func New() Resolver { return Resolver{} }
func (Resolver) Roots(project string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return home, project, nil
}
