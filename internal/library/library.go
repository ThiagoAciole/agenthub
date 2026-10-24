package library

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThiagoAciole/agenthub/internal/fsutil"
)

type Entry struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Type            string      `json:"type"`
	Origin          string      `json:"origin"`
	SHA256          string      `json:"sha256"`
	ImportedAt      time.Time   `json:"imported_at"`
	Revisions       []Revision  `json:"revisions,omitempty"`
	CurrentRevision Revision    `json:"current_revision,omitempty"`
	Status          string      `json:"status,omitempty"`
	DeletedAt       *time.Time  `json:"deleted_at,omitempty"`
	TrashSnapshot   string      `json:"trash_snapshot,omitempty"`
	LastOperationID string      `json:"last_operation_id,omitempty"`
	Source          *SourceInfo `json:"source,omitempty"`
}

type SourceInfo struct {
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Subpath string `json:"subpath,omitempty"`
}

type AddPlan struct {
	Version     int      `json:"schema_version"`
	Root        string   `json:"library_root"`
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Target      string   `json:"target_path"`
	SourceKind  string   `json:"source_kind"`
	SourcePath  string   `json:"source_path,omitempty"`
	GitURL      string   `json:"git_url,omitempty"`
	GitRef      string   `json:"git_ref,omitempty"`
	GitCommit   string   `json:"git_commit,omitempty"`
	GitPath     string   `json:"git_path,omitempty"`
	ContentHash string   `json:"content_hash"`
	Files       []string `json:"files"`
	Risk        string   `json:"risk"`
	PlanHash    string   `json:"plan_hash"`
}

type Revision struct {
	ID        string    `json:"id"`
	SHA256    string    `json:"sha256"`
	Snapshot  string    `json:"snapshot"`
	CreatedAt time.Time `json:"created_at"`
}

type RevisionRecord struct {
	Revision
	Current bool `json:"current"`
}

type History struct {
	ResourceID string           `json:"resource_id"`
	Type       string           `json:"type"`
	Name       string           `json:"name"`
	Status     string           `json:"status"`
	Revisions  []RevisionRecord `json:"revisions"`
}

type UpdatePlan struct {
	Version    int    `json:"schema_version"`
	ResourceID string `json:"resource_id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Root       string `json:"library_root"`
	Source     string `json:"source_path"`
	Target     string `json:"target_path"`
	BeforeHash string `json:"before_hash"`
	AfterHash  string `json:"after_hash"`
	Snapshot   string `json:"snapshot"`
	Action     string `json:"action"`
	PlanHash   string `json:"plan_hash"`
}

type RemovePlan struct {
	Version     int    `json:"schema_version"`
	ResourceID  string `json:"resource_id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Root        string `json:"library_root"`
	Target      string `json:"target_path"`
	ContentHash string `json:"content_hash"`
	Snapshot    string `json:"snapshot"`
	Risk        string `json:"risk"`
	PlanHash    string `json:"plan_hash"`
}

type RestorePlan struct {
	Version     int    `json:"schema_version"`
	ResourceID  string `json:"resource_id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Root        string `json:"library_root"`
	Target      string `json:"target_path"`
	ContentHash string `json:"content_hash"`
	Snapshot    string `json:"snapshot"`
	PlanHash    string `json:"plan_hash"`
}

func Add(root, source, typ string) (Entry, error) {
	return addResource(root, source, typ, "", nil, "")
}

func addResource(root, source, typ, name string, provenance *SourceInfo, expectedHash string) (Entry, error) {
	return addResourceWithRevision(root, source, typ, name, provenance, expectedHash, "", "", "")
}

func addResourceWithRevision(root, source, typ, name string, provenance *SourceInfo, expectedHash, resourceID, revisionID, origin string) (Entry, error) {
	if !validType(typ) {
		return Entry{}, fmt.Errorf("tipo inválido: %s", typ)
	}
	if resourceID != "" && !validSyncIdentity(resourceID) || revisionID != "" && !validSyncIdentity(revisionID) {
		return Entry{}, fmt.Errorf("identidade remota inválida")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return Entry{}, err
	}
	for current := source; ; current = filepath.Dir(current) {
		part, statErr := os.Lstat(current)
		if statErr == nil && fsutil.IsLinkOrJunction(part) {
			return Entry{}, fmt.Errorf("symlink não permitido no caminho de origem: %s", current)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return Entry{}, statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	info, err := os.Lstat(source)
	if err != nil {
		return Entry{}, err
	}
	if fsutil.IsLinkOrJunction(info) {
		return Entry{}, fmt.Errorf("symlink não permitido: %s", source)
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
		return Entry{}, fmt.Errorf("nome de recurso inválido")
	}
	if err := checkAddAvailable(root, typ, name); err != nil {
		return Entry{}, err
	}
	dest := filepath.Join(root, typ, name)
	if _, err := os.Lstat(dest); err == nil {
		return Entry{}, fmt.Errorf("recurso já existe: %s", dest)
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return Entry{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".agenthub-*")
	if err != nil {
		return Entry{}, err
	}
	defer os.RemoveAll(tmp)
	contentPath := tmp
	if info.IsDir() {
		if err := copyTree(source, tmp); err != nil {
			return Entry{}, err
		}
	} else if typ == "prompts" {
		if err := copyFile(source, filepath.Join(tmp, filepath.Base(source))); err != nil {
			return Entry{}, err
		}
	} else {
		contentPath = filepath.Join(tmp, filepath.Base(source))
		if err := copyFile(source, contentPath); err != nil {
			return Entry{}, err
		}
	}
	hash, err := hashTree(contentPath)
	if err != nil {
		return Entry{}, err
	}
	if expectedHash != "" && hash != expectedHash {
		return Entry{}, fmt.Errorf("conteúdo mudou durante add; nada foi gravado")
	}
	if err := rejectSymlinkPath(dest); err != nil {
		return Entry{}, err
	}
	if err := checkAddAvailable(root, typ, name); err != nil {
		return Entry{}, err
	}
	if _, err := os.Lstat(dest); err == nil {
		return Entry{}, fmt.Errorf("recurso surgiu após prévia: %s", dest)
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}
	if err := fsutil.RenameNoReplace(contentPath, dest); err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC()
	if resourceID == "" {
		resourceID, err = newResourceID()
		if err != nil {
			return Entry{}, err
		}
	} else if !validSyncIdentity(resourceID) {
		return Entry{}, fmt.Errorf("resourceId remoto inválido")
	}
	if origin == "" {
		origin = "local"
		if provenance != nil {
			origin = provenance.Kind
		}
	}
	current := newRevision(resourceID, hash, "", now)
	if revisionID != "" {
		if !validSyncIdentity(revisionID) {
			return Entry{}, fmt.Errorf("revisionId remoto inválido")
		}
		current.ID = revisionID
	}
	e := Entry{ID: resourceID, Name: name, Type: typ, Origin: origin, SHA256: hash, ImportedAt: now, CurrentRevision: current, Source: provenance}
	if err := appendLock(root, e); err != nil {
		os.RemoveAll(dest)
		return Entry{}, err
	}
	return e, nil
}

func List(root string) ([]Entry, error) {
	entries, err := ListAll(root)
	if err != nil {
		return nil, err
	}
	live := entries[:0]
	for _, entry := range entries {
		if entry.Status != "tombstoned" {
			live = append(live, entry)
		}
	}
	return live, nil
}

func ListAll(root string) ([]Entry, error) {
	b, err := os.ReadFile(registryPath(root))
	if os.IsNotExist(err) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("lockfile inválido: %w", err)
	}
	return entries, nil
}

// AddSyncedTombstone records an approved remote deletion without touching canonical content.
func AddSyncedTombstone(root, resourceID, typ, name, revisionID, contentHash string) (Entry, error) {
	if !validSyncIdentity(resourceID) || !validSyncIdentity(revisionID) || !validType(typ) || !validPortableResourceName(name) || contentHash != "" && (len(contentHash) != 64 || !isHex(contentHash)) {
		return Entry{}, fmt.Errorf("tombstone remoto inválido")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Entry{}, err
	}
	if err := checkAddAvailable(root, typ, name); err != nil {
		return Entry{}, err
	}
	if _, err := os.Lstat(filepath.Join(root, typ, name)); err == nil {
		return Entry{}, fmt.Errorf("path do tombstone ocupado por conteúdo não gerenciado")
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}
	now := time.Now().UTC()
	deletedAt := now
	entry := Entry{ID: resourceID, Name: name, Type: typ, Origin: "cloud", SHA256: contentHash, ImportedAt: now, CurrentRevision: Revision{ID: revisionID, SHA256: contentHash, CreatedAt: now}, Status: "tombstoned", DeletedAt: &deletedAt}
	if err := appendLock(root, entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func GetHistory(root, name, typ string) (History, error) {
	if !validType(typ) || !validResourceName(name) {
		return History{}, fmt.Errorf("tipo ou nome de recurso inválido")
	}
	entries, err := ListAll(root)
	if err != nil {
		return History{}, err
	}
	for _, entry := range entries {
		if entry.Name != name || entry.Type != typ {
			continue
		}
		result := History{ResourceID: entry.ID, Type: typ, Name: name, Status: entry.Status, Revisions: make([]RevisionRecord, 0, len(entry.Revisions)+1)}
		for _, revision := range entry.Revisions {
			if revision.ID == "" {
				revision = newRevision(entry.ID, revision.SHA256, revision.Snapshot, revision.CreatedAt)
			}
			result.Revisions = append(result.Revisions, RevisionRecord{Revision: revision})
		}
		current := entry.CurrentRevision
		if current.ID == "" {
			current = currentRevision(entry)
		}
		if entry.Status == "tombstoned" && current.Snapshot == "" {
			current.Snapshot = entry.TrashSnapshot
		}
		result.Revisions = append(result.Revisions, RevisionRecord{Revision: current, Current: entry.Status != "tombstoned"})
		return result, nil
	}
	return History{}, fmt.Errorf("recurso não encontrado: %s/%s", typ, name)
}

// PlanRemove describes a canonical removal without changing the library or private state.
func PlanRemove(root, name, typ string) (RemovePlan, error) {
	if !validType(typ) || !validResourceName(name) {
		return RemovePlan{}, fmt.Errorf("tipo ou nome de recurso inválido")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return RemovePlan{}, err
	}
	target := filepath.Join(root, typ, name)
	if err := rejectSymlinkPath(target); err != nil {
		return RemovePlan{}, err
	}
	entries, err := ListAll(root)
	if err != nil {
		return RemovePlan{}, err
	}
	for _, entry := range entries {
		if entry.Type != typ || entry.Name != name {
			continue
		}
		if entry.Status == "tombstoned" {
			return RemovePlan{}, fmt.Errorf("recurso já está na lixeira: %s/%s", typ, name)
		}
		hash, err := hashTree(target)
		if err != nil {
			return RemovePlan{}, err
		}
		if hash != entry.SHA256 {
			return RemovePlan{}, fmt.Errorf("recurso tem drift externo; remove bloqueado: %s", target)
		}
		idHash := sha256.Sum256([]byte(entry.ID))
		plan := RemovePlan{Version: 1, ResourceID: entry.ID, Type: typ, Name: name, Root: root, Target: target, ContentHash: hash, Snapshot: filepath.ToSlash(filepath.Join("backups", "trash", hex.EncodeToString(idHash[:]), hash)), Risk: "Recurso canônico vai para snapshot privado; deployments em providers permanecem inalterados."}
		plan.PlanHash, err = removePlanDigest(plan)
		return plan, err
	}
	return RemovePlan{}, fmt.Errorf("recurso não gerenciado: %s/%s", typ, name)
}

// ApplyRemove snapshots and tombstones an owned canonical resource; provider deployments are untouched.
func ApplyRemove(plan RemovePlan, root, stateRoot string) (Entry, error) {
	if plan.Version != 1 {
		return Entry{}, fmt.Errorf("versão de plano de remove não suportada")
	}
	digest, err := removePlanDigest(plan)
	if err != nil {
		return Entry{}, err
	}
	if digest != plan.PlanHash {
		return Entry{}, fmt.Errorf("integridade do plano de remove inválida")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Entry{}, err
	}
	if root != plan.Root || plan.Target != filepath.Join(root, plan.Type, plan.Name) || !validType(plan.Type) || !validResourceName(plan.Name) {
		return Entry{}, fmt.Errorf("plano não corresponde à biblioteca autorizada")
	}
	idHash := sha256.Sum256([]byte(plan.ResourceID))
	if plan.Snapshot != filepath.ToSlash(filepath.Join("backups", "trash", hex.EncodeToString(idHash[:]), plan.ContentHash)) {
		return Entry{}, fmt.Errorf("caminho do snapshot inválido")
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return Entry{}, err
	}
	if err := rejectSymlinkPath(stateRoot); err != nil {
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
		if entries[i].ID == plan.ResourceID && entries[i].Name == plan.Name && entries[i].Type == plan.Type {
			index = i
			break
		}
	}
	if index >= 0 && entries[index].Status == "tombstoned" && entries[index].LastOperationID == plan.PlanHash {
		if h, err := hashTree(filepath.Join(stateRoot, filepath.FromSlash(plan.Snapshot))); err == nil && h == plan.ContentHash {
			if _, err := os.Lstat(plan.Target); os.IsNotExist(err) {
				return entries[index], nil
			}
		}
		return Entry{}, fmt.Errorf("estado do tombstone mudou após a operação")
	}
	if index < 0 || entries[index].Status == "tombstoned" {
		return Entry{}, fmt.Errorf("recurso não está ativo no registry")
	}
	entry := entries[index]
	currentHash, err := hashTree(plan.Target)
	if err != nil || currentHash != plan.ContentHash || currentHash != entry.SHA256 {
		return Entry{}, fmt.Errorf("recurso mudou após plan; remove bloqueado")
	}
	idHash = sha256.Sum256([]byte(entry.ID))
	wantSnapshot := filepath.ToSlash(filepath.Join("backups", "trash", hex.EncodeToString(idHash[:]), entry.SHA256))
	if wantSnapshot != plan.Snapshot {
		return Entry{}, fmt.Errorf("snapshot diverge do plano")
	}
	backup := filepath.Join(stateRoot, filepath.FromSlash(plan.Snapshot))
	if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
		return Entry{}, err
	}
	if _, err := os.Lstat(backup); err == nil {
		h, hashErr := hashTree(backup)
		if hashErr != nil || h != entry.SHA256 {
			return Entry{}, fmt.Errorf("snapshot preexistente diverge")
		}
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	} else {
		stage, err := os.MkdirTemp(filepath.Dir(backup), ".trash-")
		if err != nil {
			return Entry{}, err
		}
		staged := filepath.Join(stage, "content")
		if err := copyResource(plan.Target, staged); err != nil {
			os.RemoveAll(stage)
			return Entry{}, err
		}
		h, err := hashTree(staged)
		if err != nil || h != entry.SHA256 {
			os.RemoveAll(stage)
			if err != nil {
				return Entry{}, err
			}
			return Entry{}, fmt.Errorf("hash do snapshot não confere")
		}
		if err := fsutil.RenameNoReplace(staged, backup); err != nil {
			os.RemoveAll(stage)
			return Entry{}, err
		}
		os.RemoveAll(stage)
	}
	if h, err := hashTree(plan.Target); err != nil || h != entry.SHA256 {
		return Entry{}, fmt.Errorf("recurso mudou antes da remoção")
	}
	quarantine, err := os.MkdirTemp(filepath.Dir(plan.Target), ".agenthub-trash-")
	if err != nil {
		return Entry{}, err
	}
	if err := os.Remove(quarantine); err != nil {
		return Entry{}, err
	}
	if err := fsutil.RenameNoReplace(plan.Target, quarantine); err != nil {
		return Entry{}, err
	}
	if h, err := hashTree(quarantine); err != nil || h != entry.SHA256 {
		if restoreErr := fsutil.RenameNoReplace(quarantine, plan.Target); restoreErr != nil {
			return Entry{}, fmt.Errorf("destino mudou durante remove; conteúdo preservado em %s: %w", quarantine, restoreErr)
		}
		return Entry{}, fmt.Errorf("destino mudou durante remove; operação cancelada")
	}
	now := time.Now().UTC()
	entry.Status, entry.DeletedAt, entry.TrashSnapshot, entry.LastOperationID = "tombstoned", &now, plan.Snapshot, plan.PlanHash
	if entry.CurrentRevision.ID == "" {
		entry.CurrentRevision = currentRevision(entry)
	}
	if err := writeEntryAt(entries, index, entry, root); err != nil {
		if restoreErr := fsutil.RenameNoReplace(quarantine, plan.Target); restoreErr != nil {
			return Entry{}, fmt.Errorf("registry falhou; recurso preservado em %s: %w", quarantine, restoreErr)
		}
		return Entry{}, err
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return entry, fmt.Errorf("tombstone aplicado; limpeza da quarentena falhou: %w", err)
	}
	return entry, nil
}

func PlanRestore(root, stateRoot, name, typ string) (RestorePlan, error) {
	if !validType(typ) || !validResourceName(name) {
		return RestorePlan{}, fmt.Errorf("tipo ou nome de recurso inválido")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return RestorePlan{}, err
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return RestorePlan{}, err
	}
	if err := rejectSymlinkPath(stateRoot); err != nil {
		return RestorePlan{}, err
	}
	entries, err := ListAll(root)
	if err != nil {
		return RestorePlan{}, err
	}
	for _, entry := range entries {
		if entry.Type != typ || entry.Name != name {
			continue
		}
		if entry.Status != "tombstoned" || entry.TrashSnapshot == "" {
			return RestorePlan{}, fmt.Errorf("recurso não está na lixeira")
		}
		idHash := sha256.Sum256([]byte(entry.ID))
		wantSnapshot := filepath.ToSlash(filepath.Join("backups", "trash", hex.EncodeToString(idHash[:]), entry.SHA256))
		if entry.TrashSnapshot != wantSnapshot {
			return RestorePlan{}, fmt.Errorf("caminho do snapshot inválido na lixeira")
		}
		backup := filepath.Join(stateRoot, filepath.FromSlash(entry.TrashSnapshot))
		if h, err := hashTree(backup); err != nil || h != entry.SHA256 {
			return RestorePlan{}, fmt.Errorf("snapshot da lixeira ausente ou alterado")
		}
		target := filepath.Join(root, typ, name)
		if _, err := os.Lstat(target); err == nil {
			return RestorePlan{}, fmt.Errorf("destino já existe; restore bloqueado: %s", target)
		} else if !os.IsNotExist(err) {
			return RestorePlan{}, err
		}
		plan := RestorePlan{Version: 1, ResourceID: entry.ID, Type: typ, Name: name, Root: root, Target: target, ContentHash: entry.SHA256, Snapshot: entry.TrashSnapshot}
		plan.PlanHash, err = restorePlanDigest(plan)
		return plan, err
	}
	return RestorePlan{}, fmt.Errorf("recurso não encontrado na lixeira: %s/%s", typ, name)
}

func ApplyRestore(plan RestorePlan, root, stateRoot string) (Entry, error) {
	if plan.Version != 1 {
		return Entry{}, fmt.Errorf("versão de plano de restore não suportada")
	}
	digest, err := restorePlanDigest(plan)
	if err != nil {
		return Entry{}, err
	}
	if digest != plan.PlanHash {
		return Entry{}, fmt.Errorf("integridade do plano de restore inválida")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Entry{}, err
	}
	if root != plan.Root || plan.Target != filepath.Join(root, plan.Type, plan.Name) || !validType(plan.Type) || !validResourceName(plan.Name) {
		return Entry{}, fmt.Errorf("plano não corresponde à biblioteca autorizada")
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return Entry{}, err
	}
	if err := rejectSymlinkPath(stateRoot); err != nil {
		return Entry{}, err
	}
	entries, err := ListAll(root)
	if err != nil {
		return Entry{}, err
	}
	index := -1
	for i := range entries {
		if entries[i].ID == plan.ResourceID && entries[i].Type == plan.Type && entries[i].Name == plan.Name {
			index = i
			break
		}
	}
	if index >= 0 && entries[index].Status != "tombstoned" && entries[index].LastOperationID == plan.PlanHash {
		if h, err := hashTree(plan.Target); err == nil && h == plan.ContentHash {
			return entries[index], nil
		}
		return Entry{}, fmt.Errorf("estado do restore mudou após a operação")
	}
	if index < 0 || entries[index].Status != "tombstoned" || entries[index].SHA256 != plan.ContentHash || entries[index].TrashSnapshot != plan.Snapshot {
		return Entry{}, fmt.Errorf("tombstone mudou após plan")
	}
	idHash := sha256.Sum256([]byte(plan.ResourceID))
	if plan.Snapshot != filepath.ToSlash(filepath.Join("backups", "trash", hex.EncodeToString(idHash[:]), plan.ContentHash)) {
		return Entry{}, fmt.Errorf("caminho do snapshot inválido")
	}
	if _, err := os.Lstat(plan.Target); err == nil {
		return Entry{}, fmt.Errorf("destino surgiu após plan; restore bloqueado")
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}
	backup := filepath.Join(stateRoot, filepath.FromSlash(plan.Snapshot))
	if h, err := hashTree(backup); err != nil || h != plan.ContentHash {
		return Entry{}, fmt.Errorf("snapshot da lixeira ausente ou alterado")
	}
	if err := rejectSymlinkPath(plan.Target); err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(filepath.Dir(plan.Target), 0700); err != nil {
		return Entry{}, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(plan.Target), ".agenthub-restore-")
	if err != nil {
		return Entry{}, err
	}
	defer os.RemoveAll(stage)
	staged := filepath.Join(stage, "content")
	if err := copyResource(backup, staged); err != nil {
		return Entry{}, err
	}
	if h, err := hashTree(staged); err != nil || h != plan.ContentHash {
		return Entry{}, fmt.Errorf("hash restaurado no staging não confere")
	}
	if err := fsutil.RenameNoReplace(staged, plan.Target); err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC()
	entry := entries[index]
	entry.Status, entry.DeletedAt, entry.TrashSnapshot, entry.LastOperationID = "", nil, "", plan.PlanHash
	previousRevision := entry.CurrentRevision
	if previousRevision.ID == "" {
		previousRevision = currentRevision(entry)
	}
	previousRevision.Snapshot = plan.Snapshot
	entry.Revisions = append(entry.Revisions, previousRevision)
	entry.CurrentRevision = newRevision(entry.ID, entry.SHA256, plan.Snapshot, now)
	if err := writeEntryAt(entries, index, entry, root); err != nil {
		if h, hashErr := hashTree(plan.Target); hashErr == nil && h == plan.ContentHash {
			_ = os.RemoveAll(plan.Target)
		}
		return Entry{}, err
	}
	return entry, nil
}

// PlanUpdate inspects an owned resource and its proposed replacement without writing.
func PlanUpdate(root, name, source, typ string) (UpdatePlan, error) {
	if !validType(typ) || name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return UpdatePlan{}, fmt.Errorf("tipo ou nome de recurso inválido")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return UpdatePlan{}, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return UpdatePlan{}, err
	}
	for current := source; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && fsutil.IsLinkOrJunction(info) {
			return UpdatePlan{}, fmt.Errorf("symlink não permitido na origem: %s", current)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return UpdatePlan{}, statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return UpdatePlan{}, err
	}
	if fsutil.IsLinkOrJunction(sourceInfo) || (!sourceInfo.IsDir() && !sourceInfo.Mode().IsRegular()) {
		return UpdatePlan{}, fmt.Errorf("origem deve ser arquivo regular ou diretório sem symlinks")
	}
	entries, err := List(root)
	if err != nil {
		return UpdatePlan{}, err
	}
	for _, entry := range entries {
		if entry.Type != typ || entry.Name != name {
			continue
		}
		target := filepath.Join(root, typ, name)
		beforeHash, err := hashTree(target)
		if err != nil {
			return UpdatePlan{}, err
		}
		if beforeHash != entry.SHA256 {
			return UpdatePlan{}, fmt.Errorf("recurso tem drift externo; update bloqueado: %s", target)
		}
		afterHash, err := resourceContentHash(source, typ)
		if err != nil {
			return UpdatePlan{}, err
		}
		idHash := sha256.Sum256([]byte(entry.ID))
		plan := UpdatePlan{Version: 1, ResourceID: entry.ID, Type: typ, Name: name, Root: root, Source: source, Target: target, BeforeHash: beforeHash, AfterHash: afterHash, Snapshot: filepath.ToSlash(filepath.Join("backups", "library", hex.EncodeToString(idHash[:]), beforeHash)), Action: "update"}
		if beforeHash == afterHash {
			plan.Action = "noop"
		}
		plan.PlanHash, err = updatePlanDigest(plan)
		return plan, err
	}
	return UpdatePlan{}, fmt.Errorf("recurso não gerenciado: %s/%s", typ, name)
}

// Update replaces an owned resource after preserving its current bytes in private state.
func Update(root, stateRoot, name, source, typ string) (Entry, error) {
	plan, err := PlanUpdate(root, name, source, typ)
	if err != nil {
		return Entry{}, err
	}
	return ApplyUpdate(plan, root, stateRoot)
}

// ApplyUpdate revalidates an update preview before changing canonical content.
func ApplyUpdate(plan UpdatePlan, root, stateRoot string) (Entry, error) {
	return applyUpdatePlan(plan, root, stateRoot, "", "", "")
}

// ApplyUpdateRevision applies an approved revision and keeps the remote revision identity.
func ApplyUpdateRevision(plan UpdatePlan, root, stateRoot, expectedRevisionID, revisionID, origin string) (Entry, error) {
	if !validSyncIdentity(expectedRevisionID) || !validSyncIdentity(revisionID) || origin != "cloud" {
		return Entry{}, fmt.Errorf("precondição ou identidade remota inválida")
	}
	return applyUpdatePlan(plan, root, stateRoot, expectedRevisionID, revisionID, origin)
}

func applyUpdatePlan(plan UpdatePlan, root, stateRoot, expectedRevisionID, revisionID, origin string) (Entry, error) {
	digest, err := updatePlanDigest(plan)
	if err != nil {
		return Entry{}, err
	}
	if digest != plan.PlanHash {
		return Entry{}, fmt.Errorf("integridade do plano de update inválida")
	}
	current, err := PlanUpdate(root, plan.Name, plan.Source, plan.Type)
	if err != nil {
		return Entry{}, err
	}
	if current.PlanHash != plan.PlanHash {
		return Entry{}, fmt.Errorf("origem ou recurso mudou após plan; gere nova prévia")
	}
	return updateOwnedRevision(plan.Root, stateRoot, plan.Name, plan.Source, plan.Type, &plan, expectedRevisionID, revisionID, origin)
}

func updateOwned(root, stateRoot, name, source, typ string, expected *UpdatePlan) (Entry, error) {
	return updateOwnedRevision(root, stateRoot, name, source, typ, expected, "", "", "")
}

func updateOwnedRevision(root, stateRoot, name, source, typ string, expected *UpdatePlan, expectedRevisionID, revisionID, origin string) (Entry, error) {
	if !validType(typ) || name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return Entry{}, fmt.Errorf("tipo ou nome de recurso inválido")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Entry{}, err
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return Entry{}, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return Entry{}, err
	}
	for current := source; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && fsutil.IsLinkOrJunction(info) {
			return Entry{}, fmt.Errorf("symlink não permitido na origem: %s", current)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return Entry{}, statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return Entry{}, err
	}
	if fsutil.IsLinkOrJunction(sourceInfo) || (!sourceInfo.IsDir() && !sourceInfo.Mode().IsRegular()) {
		return Entry{}, fmt.Errorf("origem deve ser arquivo regular ou diretório sem symlinks")
	}
	entries, err := List(root)
	if err != nil {
		return Entry{}, err
	}
	index := -1
	for i := range entries {
		if entries[i].Type == typ && entries[i].Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return Entry{}, fmt.Errorf("recurso não gerenciado: %s/%s", typ, name)
	}
	entry := entries[index]
	dest := filepath.Join(root, typ, name)
	currentHash, err := hashTree(dest)
	if err != nil {
		return Entry{}, err
	}
	if currentHash != entry.SHA256 {
		return Entry{}, fmt.Errorf("recurso tem drift externo; update bloqueado: %s", dest)
	}
	if expected != nil && (root != expected.Root || dest != expected.Target || entry.ID != expected.ResourceID || currentHash != expected.BeforeHash) {
		return Entry{}, fmt.Errorf("precondições do plano de update mudaram")
	}
	if expectedRevisionID != "" && currentRevision(entry).ID != expectedRevisionID {
		return Entry{}, fmt.Errorf("revisão local mudou após o plano de sincronização")
	}
	stageContainer, err := os.MkdirTemp(filepath.Dir(dest), ".agenthub-update-")
	if err != nil {
		return Entry{}, err
	}
	defer os.RemoveAll(stageContainer)
	stage := stageContainer
	if sourceInfo.IsDir() {
		if err := copyTree(source, stage); err != nil {
			return Entry{}, err
		}
	} else if typ == "prompts" {
		if err := copyFile(source, filepath.Join(stageContainer, filepath.Base(source))); err != nil {
			return Entry{}, err
		}
	} else {
		stage = filepath.Join(stageContainer, filepath.Base(source))
		if err := copyFile(source, stage); err != nil {
			return Entry{}, err
		}
	}
	newHash, err := hashTree(stage)
	if err != nil {
		return Entry{}, err
	}
	if expected != nil && newHash != expected.AfterHash {
		return Entry{}, fmt.Errorf("origem mudou durante update; nada foi substituído")
	}
	if expectedRevisionID != "" {
		latest, err := ListAll(root)
		if err != nil {
			return Entry{}, err
		}
		found := false
		for _, candidate := range latest {
			if candidate.ID == entry.ID {
				found = true
				if candidate.Status == "tombstoned" || candidate.SHA256 != entry.SHA256 || currentRevision(candidate).ID != expectedRevisionID {
					return Entry{}, fmt.Errorf("recurso mudou durante a sincronização")
				}
				break
			}
		}
		if !found {
			return Entry{}, fmt.Errorf("recurso deixou de existir durante a sincronização")
		}
	}
	if newHash == entry.SHA256 {
		return entry, nil
	}
	idHash := sha256.Sum256([]byte(entry.ID))
	snapshot := filepath.ToSlash(filepath.Join("backups", "library", hex.EncodeToString(idHash[:]), entry.SHA256))
	if expected != nil && snapshot != expected.Snapshot {
		return Entry{}, fmt.Errorf("snapshot diverge do plano de update")
	}
	backup := filepath.Join(stateRoot, filepath.FromSlash(snapshot))
	if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
		return Entry{}, err
	}
	if _, err := os.Lstat(backup); err == nil {
		backupHash, hashErr := hashTree(backup)
		if hashErr != nil || backupHash != entry.SHA256 {
			return Entry{}, fmt.Errorf("snapshot preexistente diverge: %s", backup)
		}
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	} else {
		backupContainer, err := os.MkdirTemp(filepath.Dir(backup), ".snapshot-")
		if err != nil {
			return Entry{}, err
		}
		defer os.RemoveAll(backupContainer)
		backupStage := filepath.Join(backupContainer, "content")
		if err := copyResource(dest, backupStage); err != nil {
			return Entry{}, err
		}
		backupHash, err := hashTree(backupStage)
		if err != nil || backupHash != entry.SHA256 {
			if err != nil {
				return Entry{}, err
			}
			return Entry{}, fmt.Errorf("hash do snapshot não confere")
		}
		if err := fsutil.RenameNoReplace(backupStage, backup); err != nil {
			os.RemoveAll(backupStage)
			return Entry{}, err
		}
	}
	quarantine, err := os.MkdirTemp(filepath.Dir(dest), ".agenthub-previous-")
	if err != nil {
		return Entry{}, err
	}
	if err := os.Remove(quarantine); err != nil {
		return Entry{}, err
	}
	if err := fsutil.RenameNoReplace(dest, quarantine); err != nil {
		return Entry{}, err
	}
	if err := fsutil.RenameNoReplace(stage, dest); err != nil {
		if restoreErr := fsutil.RenameNoReplace(quarantine, dest); restoreErr != nil {
			return Entry{}, fmt.Errorf("update falhou; versão anterior preservada em %s: %w", quarantine, restoreErr)
		}
		return Entry{}, err
	}
	deployedHash, err := hashTree(dest)
	if err != nil || deployedHash != newHash {
		if err == nil {
			err = fmt.Errorf("hash pós-update não confere")
		}
		if got, hashErr := hashTree(dest); hashErr == nil && got == newHash {
			_ = os.RemoveAll(dest)
			if restoreErr := fsutil.RenameNoReplace(quarantine, dest); restoreErr != nil {
				return Entry{}, fmt.Errorf("update falhou; versão anterior preservada em %s: %w", quarantine, restoreErr)
			}
		} else {
			return Entry{}, fmt.Errorf("update parcial; versões preservadas em %s e %s: %w", dest, quarantine, err)
		}
		return Entry{}, err
	}
	previousRevision := currentRevision(entry)
	previousRevision.Snapshot = snapshot
	entries[index].Revisions = append(entries[index].Revisions, previousRevision)
	entries[index].SHA256 = newHash
	if origin == "" {
		origin = "local"
	}
	entries[index].Origin = origin
	now := time.Now().UTC()
	entries[index].CurrentRevision = newRevision(entry.ID, newHash, "", now)
	if revisionID != "" {
		entries[index].CurrentRevision.ID = revisionID
	}
	if err := writeEntries(root, entries); err != nil {
		if got, hashErr := hashTree(dest); hashErr == nil && got == newHash {
			_ = os.RemoveAll(dest)
			if restoreErr := fsutil.RenameNoReplace(quarantine, dest); restoreErr != nil {
				return Entry{}, fmt.Errorf("registry falhou; versão anterior preservada em %s: %w", quarantine, restoreErr)
			}
		} else {
			return Entry{}, fmt.Errorf("registry falhou; update parcial preservado em %s: %w", dest, err)
		}
		return Entry{}, err
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return entries[index], fmt.Errorf("update aplicado; limpeza da cópia temporária falhou: %w", err)
	}
	return entries[index], nil
}

func copyResource(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fsutil.IsLinkOrJunction(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
		return fmt.Errorf("tipo de recurso não permitido: %s", src)
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
		return err
	}
	return copyTree(src, dst)
}

func validType(typ string) bool {
	return typ == "agents" || typ == "skills" || typ == "prompts" || typ == "commands" || typ == "rules" || typ == "memory" || typ == "mcps" || typ == "plugins" || typ == "knowledge" || typ == "profiles"
}

func validSyncIdentity(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 256 || strings.ContainsAny(value, `/\\`) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validResourceName(name string) bool {
	return name != "" && name != "." && name != ".." && strings.TrimSpace(name) == name && !strings.ContainsAny(name, `/\\`) && !strings.HasSuffix(name, ".")
}

func validPortableResourceName(name string) bool {
	if !validResourceName(name) || strings.EqualFold(name, ".git") || strings.ContainsAny(name, `<>:"|?*`) {
		return false
	}
	for _, r := range name {
		if r < 32 {
			return false
		}
	}
	device := strings.ToUpper(strings.TrimRight(strings.SplitN(name, ".", 2)[0], " ."))
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

func currentRevision(entry Entry) Revision {
	if entry.CurrentRevision.ID != "" {
		return entry.CurrentRevision
	}
	created := entry.ImportedAt
	if created.IsZero() {
		created = time.Unix(0, 0).UTC()
	}
	return newRevision(entry.ID, entry.SHA256, "", created)
}

func CurrentRevision(entry Entry) Revision { return currentRevision(entry) }

func newRevision(resourceID, hash, snapshot string, created time.Time) Revision {
	created = created.UTC()
	sum := sha256.Sum256([]byte(resourceID + "\x00" + hash + "\x00" + created.Format(time.RFC3339Nano)))
	return Revision{ID: hex.EncodeToString(sum[:]), SHA256: hash, Snapshot: snapshot, CreatedAt: created}
}

func newResourceID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func rejectSymlinkPath(path string) error {
	return fsutil.RejectLinkPath(path)
}

func writeEntryAt(entries []Entry, index int, entry Entry, root string) error {
	entries[index] = entry
	return writeEntries(root, entries)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if fsutil.IsLinkOrJunction(info) {
			return fmt.Errorf("symlink ou junction não permitido: %s", path)
		}
		if info.IsDir() {
			if rel != "." {
				return os.Mkdir(target, 0700)
			}
			return nil
		}
		return copyFile(path, target)
	})
}
func copyFile(src, dst string) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return e
	}
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}
func hashTree(path string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if fsutil.IsLinkOrJunction(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("tipo de arquivo não permitido: %s", p)
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(path, p)
		io.WriteString(h, filepath.ToSlash(rel)+"\x00")
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		_, e = io.Copy(h, f)
		f.Close()
		return e
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func appendLock(root string, e Entry) error {
	p := registryPath(root)
	entries := []Entry{}
	if b, err := os.ReadFile(p); err == nil {
		if err = json.Unmarshal(b, &entries); err != nil {
			return fmt.Errorf("lockfile inválido: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, existing := range entries {
		if existing.ID == e.ID {
			return fmt.Errorf("recurso já registrado, inclusive na lixeira: %s", e.ID)
		}
	}
	entries = append(entries, e)
	return writeEntries(root, entries)
}

func writeEntries(root string, entries []Entry) error {
	p := registryPath(root)
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".registry-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func registryPath(root string) string {
	return filepath.Join(root, ".agenthub", "registry.json")
}

func updatePlanDigest(plan UpdatePlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func removePlanDigest(plan RemovePlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func restorePlanDigest(plan RestorePlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
