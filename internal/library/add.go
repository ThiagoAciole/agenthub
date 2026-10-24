package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ThiagoAciole/agenthub/internal/fsutil"
)

const maxAddFiles = 10000
const maxAddBytes = 512 << 20

func PlanAddLocal(root, source, typ, name string) (AddPlan, error) {
	if !validType(typ) {
		return AddPlan{}, fmt.Errorf("tipo inválido: %s", typ)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return AddPlan{}, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return AddPlan{}, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return AddPlan{}, err
	}
	if name == "" {
		name = filepath.Base(source)
		if info.IsDir() && strings.EqualFold(name, "SKILL.md") {
			name = filepath.Base(filepath.Dir(source))
		}
		if !info.IsDir() {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
	}
	if !validResourceName(name) {
		return AddPlan{}, fmt.Errorf("nome de recurso inválido")
	}
	if err := rejectSymlinkPath(source); err != nil {
		return AddPlan{}, err
	}
	if fsutil.IsLinkOrJunction(info) || !info.IsDir() && !info.Mode().IsRegular() {
		return AddPlan{}, fmt.Errorf("origem deve ser arquivo regular ou diretório")
	}
	files, err := resourceFiles(source)
	if err != nil {
		return AddPlan{}, err
	}
	hash, err := resourceContentHash(source, typ)
	if err != nil {
		return AddPlan{}, err
	}
	if err := checkAddAvailable(root, typ, name); err != nil {
		return AddPlan{}, err
	}
	plan := AddPlan{Version: 1, Root: root, Type: typ, Name: name, Target: filepath.Join(root, typ, name), SourceKind: "local", SourcePath: source, ContentHash: hash, Files: files, Risk: "Copia o recurso local revisado; não executa arquivos importados."}
	plan.PlanHash, err = addPlanDigest(plan)
	return plan, err
}

func resourceContentHash(source, typ string) (string, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if typ != "prompts" || info.IsDir() {
		return hashTree(source)
	}
	h := sha256.New()
	if _, err := io.WriteString(h, filepath.Base(source)+"\x00"); err != nil {
		return "", err
	}
	file, err := os.Open(source)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(h, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ApplyAdd(ctx context.Context, plan AddPlan, root string) (Entry, error) {
	if plan.Version != 1 {
		return Entry{}, fmt.Errorf("versão de plano add não suportada")
	}
	digest, err := addPlanDigest(plan)
	if err != nil {
		return Entry{}, err
	}
	if digest != plan.PlanHash {
		return Entry{}, fmt.Errorf("integridade do plano add inválida")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Entry{}, err
	}
	if root != plan.Root || plan.Target != filepath.Join(root, plan.Type, plan.Name) || !validType(plan.Type) || !validResourceName(plan.Name) {
		return Entry{}, fmt.Errorf("plano não corresponde à biblioteca autorizada")
	}
	if err := checkAddAvailable(root, plan.Type, plan.Name); err != nil {
		return Entry{}, err
	}
	var source string
	var provenance *SourceInfo
	cleanup := func() {}
	switch plan.SourceKind {
	case "local":
		current, err := PlanAddLocal(root, plan.SourcePath, plan.Type, plan.Name)
		if err != nil {
			return Entry{}, err
		}
		if current.PlanHash != plan.PlanHash {
			return Entry{}, fmt.Errorf("origem local mudou após prévia")
		}
		source = plan.SourcePath
	case "git":
		resource, err := fetchGitResource(ctx, plan.GitURL, plan.GitRef, plan.GitPath)
		if err != nil {
			return Entry{}, err
		}
		cleanup = func() { _ = os.RemoveAll(resource.Workspace) }
		defer cleanup()
		current, err := buildGitAddPlan(root, resource, plan.Type, plan.Name)
		if err != nil {
			return Entry{}, err
		}
		if current.PlanHash != plan.PlanHash {
			return Entry{}, fmt.Errorf("ref Git mudou ou divergiu após prévia; gere novo plano")
		}
		source = resource.ContentPath
		provenance = &SourceInfo{Kind: "git", URL: resource.ProvenanceURL, Ref: resource.Ref, Commit: resource.Commit, Subpath: resource.Subpath}
	default:
		return Entry{}, fmt.Errorf("origem do plano add inválida")
	}
	entry, err := addResource(root, source, plan.Type, plan.Name, provenance, plan.ContentHash)
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// ApplyAddRevision imports verified local bytes while preserving a remote resource and revision identity.
func ApplyAddRevision(plan AddPlan, root, resourceID, revisionID string) (Entry, error) {
	if !validSyncIdentity(resourceID) || !validSyncIdentity(revisionID) {
		return Entry{}, fmt.Errorf("identidade remota inválida")
	}
	if plan.Version != 1 || plan.SourceKind != "local" {
		return Entry{}, fmt.Errorf("plano add remoto inválido")
	}
	digest, err := addPlanDigest(plan)
	if err != nil || digest != plan.PlanHash {
		return Entry{}, fmt.Errorf("integridade do plano add inválida")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Entry{}, err
	}
	if root != plan.Root || plan.Target != filepath.Join(root, plan.Type, plan.Name) {
		return Entry{}, fmt.Errorf("plano não corresponde à biblioteca autorizada")
	}
	current, err := PlanAddLocal(root, plan.SourcePath, plan.Type, plan.Name)
	if err != nil {
		return Entry{}, err
	}
	if current.PlanHash != plan.PlanHash {
		return Entry{}, fmt.Errorf("origem remota mudou após prévia")
	}
	return addResourceWithRevision(root, plan.SourcePath, plan.Type, plan.Name, nil, plan.ContentHash, resourceID, revisionID, "cloud")
}

func checkAddAvailable(root, typ, name string) error {
	if !validPortableResourceName(name) {
		return fmt.Errorf("nome de recurso inválido ou não portátil: %q", name)
	}
	target := filepath.Join(root, typ, name)
	if err := rejectSymlinkPath(target); err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("recurso já existe: %s", target)
	} else if !os.IsNotExist(err) {
		return err
	}
	siblings, err := os.ReadDir(filepath.Join(root, typ))
	if err == nil {
		names := make([]string, 0, len(siblings))
		for _, sibling := range siblings {
			names = append(names, sibling.Name())
		}
		if hasCaseInsensitiveName(names, name) {
			return fmt.Errorf("colisão de nome case-insensitive em %s: %s", filepath.Join(root, typ), name)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := ListAll(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Type, typ) && strings.EqualFold(entry.Name, name) {
			return fmt.Errorf("recurso já registrado, inclusive na lixeira: %s/%s", typ, name)
		}
	}
	return nil
}

func hasCaseInsensitiveName(names []string, name string) bool {
	for _, existing := range names {
		if strings.EqualFold(existing, name) {
			return true
		}
	}
	return false
}

func resourceFiles(root string) ([]string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if fsutil.IsLinkOrJunction(info) || !info.Mode().IsRegular() || strings.EqualFold(filepath.Base(root), ".git") {
			return nil, fmt.Errorf("tipo de arquivo não permitido: %s", root)
		}
		if err := validatePortableRelativePath(filepath.Base(root)); err != nil {
			return nil, err
		}
		if info.Size() > maxAddBytes {
			return nil, fmt.Errorf("recurso excede o limite de tamanho")
		}
		return []string{filepath.Base(root)}, nil
	}
	files := make([]string, 0)
	seen := make(map[string]bool)
	var total int64
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if fsutil.IsLinkOrJunction(info) || !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("tipo de arquivo não permitido: %s", path)
		}
		if info.IsDir() {
			if strings.EqualFold(entry.Name(), ".git") {
				return fmt.Errorf("não importe a metadata .git como conteúdo de recurso")
			}
			return nil
		}
		if strings.EqualFold(entry.Name(), ".git") {
			return fmt.Errorf("não importe a metadata .git como conteúdo de recurso")
		}
		total += info.Size()
		if len(files) >= maxAddFiles || total > maxAddBytes {
			return fmt.Errorf("recurso excede os limites de importação")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := validatePortableRelativePath(rel); err != nil {
			return err
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return fmt.Errorf("colisão de caminhos case-insensitive: %s", rel)
		}
		seen[key] = true
		files = append(files, rel)
		return nil
	})
	return files, err
}

func addPlanDigest(plan AddPlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
