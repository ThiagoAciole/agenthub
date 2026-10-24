package discovery

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ThiagoAciole/agenthub/internal/models"
	"github.com/ThiagoAciole/agenthub/internal/providers"
)

func Scan(roots []models.Root, adapters []providers.Provider) ([]models.Item, error) {
	known := map[string]bool{}
	for _, p := range adapters {
		known[p.Name()] = true
	}
	items := []models.Item{}
	for _, root := range roots {
		if !known[root.Provider] {
			continue
		}
		if info, e := os.Stat(root.Path); e == nil && !info.IsDir() {
			var found []models.Item
			var e2 error
			switch strings.ToLower(filepath.Ext(root.Path)) {
			case ".json":
				found, e2 = MCPNames(root.Path, root.Provider, root.Scope)
			case ".toml":
				found, e2 = CodexMCPNames(root.Path, root.Scope)
			}
			if e2 != nil {
				return nil, e2
			}
			items = append(items, found...)
			if filepath.Base(root.Path) == "AGENTS.md" {
				items = append(items, models.Item{Provider: root.Provider, Type: models.Rule, Name: "AGENTS", Origin: root.Scope, Path: root.Path})
			}
			continue
		}
		err := filepath.WalkDir(root.Path, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			typ, ok := classify(filepath.Base(root.Path), path)
			if !ok {
				return nil
			}
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if name == "" {
				return nil
			}
			items = append(items, models.Item{Provider: root.Provider, Type: typ, Name: name, Origin: root.Scope, Path: path})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func CodexMCPNames(path, scope string) ([]models.Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	items := []models.Item{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "[mcp_servers.") || !strings.HasSuffix(line, "]") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(line, "[mcp_servers."), "]")
		name = strings.Trim(name, "\"'")
		if name != "" {
			items = append(items, models.Item{Provider: "codex", Type: models.MCP, Name: name, Origin: scope, Path: path})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func classify(root, path string) (models.Type, bool) {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))
	if ext != ".md" && ext != ".json" && ext != ".yaml" && ext != ".yml" {
		return "", false
	}
	switch strings.ToLower(root) {
	case "agents":
		if ext == ".md" {
			return models.Agent, true
		}
	case "skills":
		if ext == ".md" {
			return models.Skill, true
		}
	case "commands", "prompts":
		if ext == ".md" {
			return models.Command, true
		}
	case "rules":
		if ext == ".md" {
			return models.Rule, true
		}
	}
	return "", false
}

// MCPNames returns only map keys from the mcpServers object; values are discarded.
func MCPNames(path, provider, scope string) ([]models.Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, io.ErrUnexpectedEOF
	}
	items := []models.Item{}
	for dec.More() {
		keyToken, e := dec.Token()
		if e != nil {
			return nil, e
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		if key == "mcpServers" || key == "servers" {
			t, e := dec.Token()
			if e != nil {
				return nil, e
			}
			if t == json.Delim('{') {
				for dec.More() {
					n, e := dec.Token()
					if e != nil {
						return nil, e
					}
					name, ok := n.(string)
					if !ok {
						return nil, io.ErrUnexpectedEOF
					}
					items = append(items, models.Item{Provider: provider, Type: models.MCP, Name: name, Origin: scope, Path: path})
					if e = skipValue(dec); e != nil {
						return nil, e
					}
				}
				_, e = dec.Token()
				if e != nil {
					return nil, e
				}
			} else if e = skipAfterToken(dec, t); e != nil {
				return nil, e
			}
		} else if e := skipValue(dec); e != nil {
			return nil, e
		}
	}
	return items, nil
}

func skipValue(dec *json.Decoder) error {
	t, e := dec.Token()
	if e != nil {
		return e
	}
	return skipAfterToken(dec, t)
}
func skipAfterToken(dec *json.Decoder, t json.Token) error {
	d, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if d == '{' {
		for dec.More() {
			if _, e := dec.Token(); e != nil {
				return e
			}
			if e := skipValue(dec); e != nil {
				return e
			}
		}
		_, e := dec.Token()
		return e
	}
	if d == '[' {
		for dec.More() {
			if e := skipValue(dec); e != nil {
				return e
			}
		}
		_, e := dec.Token()
		return e
	}
	return nil
}

func Hash(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
