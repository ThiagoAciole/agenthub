package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ThiagoAciole/agenthub/internal/fsutil"
)

type RenamePlan struct {
	Version     int    `json:"schema_version"`
	ResourceID  string `json:"resource_id"`
	Type        string `json:"type"`
	From        string `json:"from"`
	To          string `json:"to"`
	Root        string `json:"library_root"`
	Source      string `json:"source_path"`
	Target      string `json:"target_path"`
	ContentHash string `json:"content_hash"`
	PlanHash    string `json:"plan_hash"`
}

func PlanRename(root, name, typ, newName string) (RenamePlan, error) {
	if !validType(typ) || !validResourceName(name) || !validPortableResourceName(newName) {
		return RenamePlan{}, fmt.Errorf("tipo ou nome de recurso inválido")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return RenamePlan{}, err
	}
	if strings.EqualFold(name, newName) {
		return RenamePlan{}, fmt.Errorf("nome de destino é igual ao nome atual")
	}
	entries, err := ListAll(root)
	if err != nil {
		return RenamePlan{}, err
	}
	var entry *Entry
	for i := range entries {
		if entries[i].Type == typ && entries[i].Name == name && entries[i].Status != "tombstoned" {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return RenamePlan{}, fmt.Errorf("recurso ativo não encontrado: %s/%s", typ, name)
	}
	if err := checkAddAvailable(root, typ, newName); err != nil {
		return RenamePlan{}, err
	}
	source, target := filepath.Join(root, typ, name), filepath.Join(root, typ, newName)
	if err := rejectSymlinkPath(source); err != nil {
		return RenamePlan{}, err
	}
	hash, err := hashTree(source)
	if err != nil {
		return RenamePlan{}, err
	}
	if hash != entry.SHA256 {
		return RenamePlan{}, fmt.Errorf("recurso tem drift externo; rename bloqueado: %s", source)
	}
	plan := RenamePlan{Version: 1, ResourceID: entry.ID, Type: typ, From: name, To: newName, Root: root, Source: source, Target: target, ContentHash: hash}
	plan.PlanHash, err = renamePlanDigest(plan)
	return plan, err
}

func ApplyRename(plan RenamePlan, root string) (Entry, error) {
	if plan.Version != 1 {
		return Entry{}, fmt.Errorf("versão de plano rename não suportada")
	}
	digest, err := renamePlanDigest(plan)
	if err != nil || digest != plan.PlanHash {
		return Entry{}, fmt.Errorf("integridade do plano rename inválida")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Entry{}, err
	}
	if root != plan.Root || !validType(plan.Type) || !validResourceName(plan.From) || !validPortableResourceName(plan.To) || plan.Source != filepath.Join(root, plan.Type, plan.From) || plan.Target != filepath.Join(root, plan.Type, plan.To) || strings.EqualFold(plan.From, plan.To) {
		return Entry{}, fmt.Errorf("plano não corresponde à biblioteca autorizada")
	}
	if err := rejectSymlinkPath(plan.Source); err != nil {
		return Entry{}, err
	}
	if err := rejectSymlinkPath(plan.Target); err != nil {
		return Entry{}, err
	}
	entries, err := ListAll(root)
	if err != nil {
		return Entry{}, err
	}
	index := -1
	for i := range entries {
		if entries[i].ID == plan.ResourceID && entries[i].Type == plan.Type && entries[i].Name == plan.From && entries[i].Status != "tombstoned" {
			index = i
			break
		}
	}
	if index < 0 {
		return Entry{}, fmt.Errorf("recurso mudou após plan; rename bloqueado")
	}
	if err := checkAddAvailable(root, plan.Type, plan.To); err != nil {
		return Entry{}, err
	}
	hash, err := hashTree(plan.Source)
	if err != nil || hash != plan.ContentHash || hash != entries[index].SHA256 {
		return Entry{}, fmt.Errorf("recurso mudou após plan; rename bloqueado")
	}
	if err := fsutil.RenameNoReplace(plan.Source, plan.Target); err != nil {
		return Entry{}, err
	}
	entries[index].Name = plan.To
	if err := writeEntries(root, entries); err != nil {
		if rollbackErr := fsutil.RenameNoReplace(plan.Target, plan.Source); rollbackErr != nil {
			return Entry{}, fmt.Errorf("registry falhou; recurso preservado em %s: %w", plan.Target, rollbackErr)
		}
		return Entry{}, err
	}
	return entries[index], nil
}

func renamePlanDigest(plan RenamePlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}
