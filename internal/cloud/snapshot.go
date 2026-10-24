package cloud

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ThiagoAciole/agenthub/internal/fsutil"
	"github.com/ThiagoAciole/agenthub/internal/library"
)

const maxSnapshotFiles = 10000
const maxSnapshotBytes = 512 << 20
const maxSnapshotResources = 10000
const maxSnapshotManifestBytes = 16 << 20

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password)\s*[:=]\s*["']?([^\s"']{8,})`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{16,}`),
}
var envReference = regexp.MustCompile(`^\$(?:[A-Za-z_][A-Za-z0-9_]*|\{[A-Za-z_][A-Za-z0-9_]*\})$`)

type Snapshot struct {
	Version   int                `json:"schema_version"`
	Resources []SnapshotResource `json:"resources"`
}

type SnapshotResource struct {
	ResourceID string `json:"resource_id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	RevisionID string `json:"revision_id"`
	Hash       string `json:"content_hash"`
	Tombstone  bool   `json:"tombstone,omitempty"`
}

// BuildSnapshot exports only resources explicitly selected by the caller into a new staging directory.
func BuildSnapshot(root, destination string, selected map[string]bool) (Snapshot, error) {
	if len(selected) == 0 {
		return Snapshot{}, fmt.Errorf("selecione ao menos um resourceId para exportar")
	}
	if len(selected) > maxSnapshotResources {
		return Snapshot{}, fmt.Errorf("snapshot excede o limite de recursos")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return Snapshot{}, err
	}
	if err := fsutil.RejectLinkPath(root); err != nil {
		return Snapshot{}, err
	}
	if err := fsutil.RejectLinkPath(filepath.Join(root, ".agenthub", "registry.json")); err != nil {
		return Snapshot{}, err
	}
	if err := fsutil.RejectLinkPath(filepath.Dir(destination)); err != nil {
		return Snapshot{}, err
	}
	if pathsOverlap(root, destination) {
		return Snapshot{}, fmt.Errorf("staging Git deve ficar fora da biblioteca canônica")
	}
	entries, err := library.ListAll(root)
	if err != nil {
		return Snapshot{}, err
	}
	byID := make(map[string]library.Entry, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.Name == "" || entry.Type == "" {
			return Snapshot{}, fmt.Errorf("registry contém recurso sem identidade ou path válido")
		}
		if _, exists := byID[entry.ID]; exists {
			return Snapshot{}, fmt.Errorf("resourceId duplicado no registry: %s", entry.ID)
		}
		byID[entry.ID] = entry
	}
	for id := range selected {
		if !selected[id] {
			return Snapshot{}, fmt.Errorf("resourceId não aprovado para exportação: %s", id)
		}
		if _, exists := byID[id]; !exists {
			return Snapshot{}, fmt.Errorf("resourceId não registrado: %s", id)
		}
	}
	paths := make(map[string]string, len(selected))
	for id := range selected {
		entry := byID[id]
		if !validSyncType(entry.Type) || !validSyncName(entry.Name) {
			return Snapshot{}, fmt.Errorf("resource path inválido para %s", id)
		}
		key := strings.ToLower(filepath.Join(entry.Type, entry.Name))
		if previous, exists := paths[key]; exists {
			return Snapshot{}, fmt.Errorf("resource path duplicado em %s e %s", previous, id)
		}
		paths[key] = id
	}
	if _, err := os.Lstat(destination); err == nil {
		return Snapshot{}, fmt.Errorf("destino de snapshot já existe: %s", destination)
	} else if !os.IsNotExist(err) {
		return Snapshot{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return Snapshot{}, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".agenthub-snapshot-*")
	if err != nil {
		return Snapshot{}, err
	}
	defer os.RemoveAll(stage)

	snapshot := Snapshot{Version: 1, Resources: make([]SnapshotResource, 0, len(selected))}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var total int64
	files := 0
	for _, id := range ids {
		entry := byID[id]
		resource := SnapshotResource{ResourceID: id, Type: entry.Type, Name: entry.Name, RevisionID: library.CurrentRevision(entry).ID, Hash: entry.SHA256, Tombstone: entry.Status == "tombstoned"}
		if entry.Status != "tombstoned" {
			source := filepath.Join(root, entry.Type, entry.Name)
			if err := validateResourcePath(root, source); err != nil {
				return Snapshot{}, err
			}
			hash, size, count, err := copyResource(source, filepath.Join(stage, entry.Type, entry.Name))
			if err != nil {
				return Snapshot{}, fmt.Errorf("exportando %s: %w", id, err)
			}
			if hash != entry.SHA256 {
				return Snapshot{}, fmt.Errorf("recurso %s mudou desde o registry; exportação bloqueada", id)
			}
			total += size
			files += count
			if total > maxSnapshotBytes || files > maxSnapshotFiles {
				return Snapshot{}, fmt.Errorf("snapshot excede o limite de arquivos ou bytes")
			}
		}
		snapshot.Resources = append(snapshot.Resources, resource)
	}
	manifest, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return Snapshot{}, err
	}
	if len(manifest)+1 > maxSnapshotManifestBytes {
		return Snapshot{}, fmt.Errorf("manifesto do snapshot excede o limite de bytes")
	}
	if err := os.WriteFile(filepath.Join(stage, "agenthub.manifest.json"), append(manifest, '\n'), 0600); err != nil {
		return Snapshot{}, err
	}
	if err := fsutil.RenameNoReplace(stage, destination); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func validateResourcePath(root, path string) error {
	if err := fsutil.RejectLinkPath(path); err != nil {
		return err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("resource path escapa da biblioteca")
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	if len(parts) != 2 || !validSyncType(parts[0]) || !validSyncName(parts[1]) {
		return fmt.Errorf("resource path inválido")
	}
	return nil
}

func validSyncType(value string) bool {
	switch value {
	case "agents", "skills", "prompts", "commands", "rules", "memory", "mcps", "plugins", "knowledge", "profiles":
		return true
	default:
		return false
	}
}

func validSyncName(value string) bool {
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value || strings.EqualFold(value, ".git") || strings.ContainsAny(value, `/\\<>:"|?*`) || strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 {
			return false
		}
	}
	device := strings.ToUpper(strings.TrimRight(strings.SplitN(value, ".", 2)[0], " ."))
	if device == "CON" || device == "PRN" || device == "AUX" || device == "NUL" || device == "CONIN$" || device == "CONOUT$" {
		return false
	}
	for i := 1; i <= 9; i++ {
		if device == fmt.Sprintf("COM%d", i) || device == fmt.Sprintf("LPT%d", i) {
			return false
		}
	}
	return true
}

func pathsOverlap(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
		return true
	}
	rel, err = filepath.Rel(b, a)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func copyResource(source, target string) (string, int64, int, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return "", 0, 0, err
	}
	if fsutil.IsLinkOrJunction(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
		return "", 0, 0, fmt.Errorf("resource não é arquivo regular ou diretório: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", 0, 0, err
	}
	if info.IsDir() {
		if err := os.Mkdir(target, 0700); err != nil {
			return "", 0, 0, err
		}
	} else if err := os.Mkdir(target, 0700); err != nil {
		return "", 0, 0, err
	}
	h := sha256.New()
	var total int64
	files := 0
	err = filepath.WalkDir(source, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if fsutil.IsLinkOrJunction(current) || (!current.IsDir() && !current.Mode().IsRegular()) {
			return fmt.Errorf("link ou arquivo especial bloqueado: %s", path)
		}
		if current.IsDir() {
			if path != source {
				return os.Mkdir(filepath.Join(target, mustRelative(source, path)), 0700)
			}
			return nil
		}
		rel := filepath.ToSlash(mustRelative(source, path))
		base := strings.ToLower(filepath.Base(rel))
		ext := strings.ToLower(filepath.Ext(base))
		if base == ".gitattributes" {
			return fmt.Errorf(".gitattributes não é aceito no snapshot Git")
		}
		if strings.HasPrefix(base, ".env") || base == "id_rsa" || base == "id_ed25519" || base == "credentials" || base == "secrets" || ext == ".pem" || ext == ".key" || ext == ".p12" || ext == ".pfx" {
			return fmt.Errorf("arquivo sensível bloqueado: %s", filepath.Base(rel))
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		openedInfo, err := in.Stat()
		if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(current, openedInfo) {
			in.Close()
			return fmt.Errorf("arquivo mudou durante a exportação: %s", filepath.Base(rel))
		}
		outPath := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(outPath), 0700); err != nil {
			return err
		}
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		io.WriteString(h, rel+"\x00")
		tee := io.TeeReader(in, h)
		copied, copyErr := io.Copy(out, tee)
		inCloseErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inCloseErr != nil {
			return inCloseErr
		}
		if closeErr != nil {
			return closeErr
		}
		if copied > maxSnapshotBytes-total {
			return fmt.Errorf("snapshot excede o limite de bytes")
		}
		total += copied
		files++
		if files > maxSnapshotFiles {
			return fmt.Errorf("snapshot excede o limite de arquivos")
		}
		return scanForSecrets(outPath)
	})
	if err != nil {
		return "", total, files, err
	}
	return hex.EncodeToString(h.Sum(nil)), total, files, nil
}

func mustRelative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ".."
	}
	return rel
}

func scanForSecrets(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := bufio.NewReaderSize(f, 64*1024)
	var tail []byte
	for {
		chunk := make([]byte, 64*1024)
		n, readErr := reader.Read(chunk)
		if n > 0 {
			window := append(tail, chunk[:n]...)
			for _, pattern := range secretPatterns {
				for _, match := range pattern.FindAllSubmatch(window, -1) {
					if len(match) < 2 || !envReference.Match(match[1]) {
						return fmt.Errorf("padrão de credencial detectado em %s", filepath.Base(path))
					}
				}
			}
			keep := 4096
			if len(window) < keep {
				keep = len(window)
			}
			tail = append(tail[:0], window[len(window)-keep:]...)
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
