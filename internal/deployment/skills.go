package deployment

import (
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
	"github.com/ThiagoAciole/agenthub/internal/library"
)

const maxSkillBytes = 512 << 20

type SkillPlan struct {
	Version              int      `json:"schema_version"`
	ResourceID           string   `json:"resource_id"`
	Kind                 string   `json:"kind"`
	Slug                 string   `json:"slug"`
	SourceRoot           string   `json:"source_root"`
	ContentHash          string   `json:"content_hash"`
	Provider             string   `json:"provider_id"`
	Scope                string   `json:"scope"`
	TargetRoot           string   `json:"target_root"`
	TargetPath           string   `json:"target_path"`
	TargetExists         bool     `json:"target_exists"`
	TargetHash           string   `json:"target_hash,omitempty"`
	TargetAction         string   `json:"target_action"`
	PreviousDeploymentID string   `json:"previous_deployment_id,omitempty"`
	Risk                 string   `json:"risk"`
	FilesToCreate        []string `json:"files_to_create"`
	FilesToRemove        []string `json:"files_to_remove,omitempty"`
	PlanHash             string   `json:"plan_hash"`
}

type Deployment struct {
	ID                   string     `json:"deployment_id"`
	ResourceID           string     `json:"resource_id"`
	Provider             string     `json:"provider_id"`
	Scope                string     `json:"scope"`
	Slug                 string     `json:"slug"`
	TargetRoot           string     `json:"target_root"`
	TargetPath           string     `json:"target_path"`
	ContentHash          string     `json:"installed_hash"`
	Status               string     `json:"status"`
	PreviousDeploymentID string     `json:"previous_deployment_id,omitempty"`
	PreviousHash         string     `json:"previous_hash,omitempty"`
	Snapshot             string     `json:"snapshot,omitempty"`
	RemovalSnapshot      string     `json:"removal_snapshot,omitempty"`
	LastOperationID      string     `json:"last_operation_id,omitempty"`
	RemovedAt            *time.Time `json:"removed_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

// PlanSkill inspects a registered canonical skill and its proposed target without writing.
func PlanSkill(libraryRoot, slug, provider, scope, targetRoot, stateRoot string) (SkillPlan, error) {
	if !validSlug(slug) {
		return SkillPlan{}, fmt.Errorf("slug inválido: %q", slug)
	}
	if provider != "codex" && provider != "claude-code" && provider != "opencode" {
		return SkillPlan{}, fmt.Errorf("provider não reconhecido: %s", provider)
	}
	if scope != "global" && scope != "project" {
		return SkillPlan{}, fmt.Errorf("escopo inválido: %s", scope)
	}
	libraryRoot, err := filepath.Abs(libraryRoot)
	if err != nil {
		return SkillPlan{}, err
	}
	targetRoot, err = filepath.Abs(targetRoot)
	if err != nil {
		return SkillPlan{}, err
	}
	source := filepath.Join(libraryRoot, "skills", slug)
	if err := rejectSymlinkPath(source); err != nil {
		return SkillPlan{}, err
	}
	if err := rejectSymlinkPath(targetRoot); err != nil && !os.IsNotExist(err) {
		return SkillPlan{}, err
	}
	contentHash, err := hashTree(source)
	if err != nil {
		return SkillPlan{}, fmt.Errorf("lendo skill canônica: %w", err)
	}
	files, err := listFiles(source)
	if err != nil {
		return SkillPlan{}, err
	}
	entrypoint := filepath.Join(source, "SKILL.md")
	entryInfo, err := os.Stat(entrypoint)
	if err != nil || !entryInfo.Mode().IsRegular() {
		return SkillPlan{}, fmt.Errorf("skill sem SKILL.md regular: %s", source)
	}
	entries, err := library.List(libraryRoot)
	if err != nil {
		return SkillPlan{}, err
	}
	resourceID := ""
	for _, entry := range entries {
		if entry.Type == "skills" && entry.Name == slug {
			if entry.SHA256 != contentHash {
				return SkillPlan{}, fmt.Errorf("skill canônica diverge do registry: %s", slug)
			}
			resourceID = entry.ID
			break
		}
	}
	if resourceID == "" {
		return SkillPlan{}, fmt.Errorf("skill não adotada pelo AgentHub: %s", slug)
	}
	plan := SkillPlan{
		Version: 1, ResourceID: resourceID, Kind: "skill", Slug: slug,
		SourceRoot: source, ContentHash: contentHash, Provider: provider, Scope: scope,
		TargetRoot: targetRoot, TargetPath: filepath.Join(targetRoot, slug), TargetAction: "create", FilesToCreate: files,
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return SkillPlan{}, err
	}
	if _, err := os.Lstat(plan.TargetPath); err == nil {
		plan.TargetExists = true
		plan.TargetHash, err = hashTree(plan.TargetPath)
		if err != nil {
			return SkillPlan{}, fmt.Errorf("inspecionando destino: %w", err)
		}
		owner, ownerErr := findDeploymentByHash(stateRoot, provider, scope, plan.TargetPath, plan.TargetHash)
		if ownerErr != nil {
			return SkillPlan{}, ownerErr
		}
		if owner == nil || owner.ResourceID != resourceID {
			plan.TargetAction = "conflict"
			plan.Risk = "Destino existente sem ownership AgentHub comprovado; apply bloqueado."
		} else {
			plan.PreviousDeploymentID = owner.ID
			if plan.TargetHash == contentHash {
				plan.TargetAction = "noop"
				plan.Risk = "Destino já contém a revisão canônica gerenciada; nenhuma escrita necessária."
			} else {
				plan.TargetAction = "update"
				plan.Risk = "Atualiza destino gerenciado; snapshot privado da versão anterior será salvo antes da troca."
				oldFiles, err := listFiles(plan.TargetPath)
				if err != nil {
					return SkillPlan{}, err
				}
				newSet := map[string]bool{}
				for _, path := range files {
					newSet[path] = true
				}
				for _, path := range oldFiles {
					if !newSet[path] {
						plan.FilesToRemove = append(plan.FilesToRemove, path)
					}
				}
			}
		}
	} else if os.IsNotExist(err) {
		owner, ownerErr := findDeploymentByPath(stateRoot, provider, scope, plan.TargetPath)
		if ownerErr != nil {
			return SkillPlan{}, ownerErr
		}
		if owner != nil {
			plan.TargetAction = "conflict"
			plan.PreviousDeploymentID = owner.ID
			plan.Risk = "Deployment gerenciado está ausente no destino; drift deve ser resolvido explicitamente."
		} else {
			removed, removedErr := findRemovedDeploymentByPath(stateRoot, provider, scope, plan.TargetPath)
			if removedErr != nil {
				return SkillPlan{}, removedErr
			}
			if removed != nil {
				plan.PreviousDeploymentID = removed.ID
			}
			plan.Risk = "Cria somente se o destino continuar ausente; conteúdo existente não será adotado nem substituído."
		}
	} else {
		return SkillPlan{}, err
	}
	plan.PlanHash, err = planDigest(plan)
	return plan, err
}

func Apply(plan SkillPlan, allowedLibraryRoot, allowedTargetRoot, stateRoot string) (result Deployment, retErr error) {
	return applyWithRename(plan, allowedLibraryRoot, allowedTargetRoot, stateRoot, fsutil.RenameNoReplace)
}

func applyWithRename(plan SkillPlan, allowedLibraryRoot, allowedTargetRoot, stateRoot string, rename func(string, string) error) (result Deployment, retErr error) {
	if err := validatePlan(plan, allowedLibraryRoot, allowedTargetRoot); err != nil {
		return Deployment{}, err
	}
	if err := rejectSymlinkPath(plan.SourceRoot); err != nil {
		return Deployment{}, err
	}
	entries, err := library.List(allowedLibraryRoot)
	if err != nil {
		return Deployment{}, err
	}
	registered := false
	for _, entry := range entries {
		if entry.ID == plan.ResourceID && entry.Type == "skills" && entry.Name == plan.Slug && entry.SHA256 == plan.ContentHash {
			registered = true
			break
		}
	}
	if !registered {
		return Deployment{}, fmt.Errorf("plano não corresponde a uma skill registrada no canônico")
	}
	if plan.TargetAction == "conflict" {
		return Deployment{}, fmt.Errorf("plano contém conflito de ownership/drift; apply bloqueado")
	}
	if err := rejectSymlinkPath(plan.TargetRoot); err != nil && !os.IsNotExist(err) {
		return Deployment{}, err
	}
	hash, err := hashTree(plan.SourceRoot)
	if err != nil {
		return Deployment{}, err
	}
	if hash != plan.ContentHash {
		return Deployment{}, fmt.Errorf("conteúdo canônico mudou após plan")
	}
	files, err := listFiles(plan.SourceRoot)
	if err != nil {
		return Deployment{}, err
	}
	if strings.Join(files, "\x00") != strings.Join(plan.FilesToCreate, "\x00") {
		return Deployment{}, fmt.Errorf("lista de arquivos do plano diverge da skill canônica")
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return Deployment{}, err
	}
	if err := rejectSymlinkPath(stateRoot); err != nil {
		return Deployment{}, err
	}
	deploymentDir := filepath.Join(stateRoot, "deployments", plan.PlanHash)
	if _, err := os.Stat(deploymentDir); err == nil {
		return existingDeployment(deploymentDir, plan, stateRoot)
	} else if !os.IsNotExist(err) {
		return Deployment{}, err
	}
	var previous *Deployment
	switch plan.TargetAction {
	case "create":
		if plan.TargetExists {
			return Deployment{}, fmt.Errorf("destino mudou após plan; gere nova prévia")
		}
		if _, err := os.Lstat(plan.TargetPath); err == nil {
			return Deployment{}, fmt.Errorf("destino já existe e não pode ser adotado: %s", plan.TargetPath)
		} else if !os.IsNotExist(err) {
			return Deployment{}, err
		}
		if plan.PreviousDeploymentID != "" {
			removed, err := findDeploymentByID(stateRoot, plan.PreviousDeploymentID)
			if err != nil {
				return Deployment{}, err
			}
			if removed.Status != "removed" || removed.ResourceID != plan.ResourceID || removed.Provider != plan.Provider || removed.Scope != plan.Scope || removed.TargetPath != plan.TargetPath {
				return Deployment{}, fmt.Errorf("deployment anterior não foi removido ou não corresponde ao plano")
			}
		}
	case "update", "noop":
		if !plan.TargetExists || plan.PreviousDeploymentID == "" {
			return Deployment{}, fmt.Errorf("plano sem precondição/ownership anterior")
		}
		owner, err := findDeploymentByID(stateRoot, plan.PreviousDeploymentID)
		if err != nil {
			return Deployment{}, err
		}
		if owner.ResourceID != plan.ResourceID || owner.Provider != plan.Provider || owner.Scope != plan.Scope || owner.TargetPath != plan.TargetPath || owner.ContentHash != plan.TargetHash {
			return Deployment{}, fmt.Errorf("ownership mudou após plan")
		}
		currentHash, err := hashTree(plan.TargetPath)
		if err != nil || currentHash != plan.TargetHash {
			return Deployment{}, fmt.Errorf("destino gerenciado mudou após plan")
		}
		if plan.TargetAction == "noop" {
			if plan.ContentHash != plan.TargetHash {
				return Deployment{}, fmt.Errorf("plano noop tem hashes diferentes")
			}
			return owner, nil
		}
		previous = &owner
	default:
		return Deployment{}, fmt.Errorf("ação de plano inválida")
	}
	if err := os.MkdirAll(filepath.Dir(deploymentDir), 0700); err != nil {
		return Deployment{}, err
	}
	if err := rejectSymlinkPath(filepath.Dir(deploymentDir)); err != nil {
		return Deployment{}, err
	}
	if err := os.Mkdir(deploymentDir, 0700); err != nil {
		return Deployment{}, err
	}
	removeState := true
	defer func() {
		if removeState {
			_ = os.RemoveAll(deploymentDir)
		}
	}()
	operation, err := startOperation(stateRoot, plan)
	if err != nil {
		return Deployment{}, err
	}
	defer func() {
		if retErr == nil {
			operation.Status, operation.Error = "applied", ""
		} else {
			if operation.Status == "applying" {
				operation.Status = "failed"
			}
			operation.Error = retErr.Error()
		}
		operation.UpdatedAt = time.Now().UTC()
		if err := writeOperation(stateRoot, operation); err != nil {
			if retErr == nil {
				result = Deployment{}
				retErr = fmt.Errorf("deployment concluído, mas journal não foi confirmado: %w", err)
			} else {
				retErr = fmt.Errorf("%w (journal também não foi atualizado: %v)", retErr, err)
			}
		}
	}()
	createdTargetDirs, err := mkdirAllTracked(plan.TargetRoot)
	if err != nil {
		return Deployment{}, err
	}
	keepTargetDirs := false
	defer func() {
		if !keepTargetDirs {
			removeEmptyDirs(createdTargetDirs)
		}
	}()
	stage, err := os.MkdirTemp(plan.TargetRoot, ".agenthub-stage-")
	if err != nil {
		return Deployment{}, err
	}
	defer os.RemoveAll(stage)
	if err := copyTree(plan.SourceRoot, stage); err != nil {
		return Deployment{}, err
	}
	if copiedHash, err := hashTree(stage); err != nil || copiedHash != plan.ContentHash {
		if err != nil {
			return Deployment{}, err
		}
		return Deployment{}, fmt.Errorf("hash da cópia staged não confere")
	}
	var quarantine string
	snapshot := ""
	if previous != nil {
		currentHash, err := hashTree(plan.TargetPath)
		if err != nil || currentHash != plan.TargetHash {
			return Deployment{}, fmt.Errorf("destino mudou durante apply")
		}
		snapshot = filepath.ToSlash(filepath.Join("backups", "deployments", plan.PlanHash))
		backup := filepath.Join(stateRoot, filepath.FromSlash(snapshot))
		if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
			return Deployment{}, err
		}
		if _, err := os.Lstat(backup); err == nil {
			backupHash, hashErr := hashTree(backup)
			if hashErr != nil || backupHash != plan.TargetHash {
				return Deployment{}, fmt.Errorf("snapshot preexistente diverge do destino anterior")
			}
		} else if !os.IsNotExist(err) {
			return Deployment{}, err
		} else {
			backupStage, err := os.MkdirTemp(filepath.Dir(backup), ".snapshot-")
			if err != nil {
				return Deployment{}, err
			}
			if err := copyTree(plan.TargetPath, backupStage); err != nil {
				os.RemoveAll(backupStage)
				return Deployment{}, err
			}
			backupHash, err := hashTree(backupStage)
			if err != nil || backupHash != plan.TargetHash {
				os.RemoveAll(backupStage)
				if err != nil {
					return Deployment{}, err
				}
				return Deployment{}, fmt.Errorf("hash do snapshot não confere")
			}
			if err := rename(backupStage, backup); err != nil {
				os.RemoveAll(backupStage)
				return Deployment{}, err
			}
		}
		quarantine, err = os.MkdirTemp(plan.TargetRoot, ".agenthub-previous-")
		if err != nil {
			return Deployment{}, err
		}
		if err := os.Remove(quarantine); err != nil {
			return Deployment{}, err
		}
		if err := rename(plan.TargetPath, quarantine); err != nil {
			return Deployment{}, err
		}
	} else if _, err := os.Lstat(plan.TargetPath); err == nil {
		return Deployment{}, fmt.Errorf("destino surgiu durante apply: %s", plan.TargetPath)
	} else if !os.IsNotExist(err) {
		return Deployment{}, err
	}
	if err := rename(stage, plan.TargetPath); err != nil {
		if quarantine != "" {
			if restoreErr := rename(quarantine, plan.TargetPath); restoreErr != nil {
				operation.Status = "partial"
				operation.RecoveryPath = quarantine
				removeState = false
				return Deployment{}, fmt.Errorf("apply falhou; versão anterior preservada em %s: %w", quarantine, restoreErr)
			}
		}
		return Deployment{}, err
	}
	deployedHash, err := hashTree(plan.TargetPath)
	if err != nil || deployedHash != plan.ContentHash {
		if err == nil {
			err = fmt.Errorf("hash pós-apply não confere")
		}
		if got, hashErr := hashTree(plan.TargetPath); hashErr == nil && got == plan.ContentHash {
			_ = os.RemoveAll(plan.TargetPath)
			if quarantine != "" {
				if restoreErr := rename(quarantine, plan.TargetPath); restoreErr != nil {
					operation.Status = "partial"
					operation.RecoveryPath = quarantine
					removeState = false
					return Deployment{}, fmt.Errorf("versão anterior preservada em %s: %w", quarantine, restoreErr)
				}
			}
		} else {
			operation.Status = "partial"
			removeState = false
			return Deployment{}, fmt.Errorf("apply parcial preservado em %s: %w", plan.TargetPath, err)
		}
		return Deployment{}, err
	}
	deployment := Deployment{ID: plan.PlanHash, ResourceID: plan.ResourceID, Provider: plan.Provider, Scope: plan.Scope, Slug: plan.Slug, TargetRoot: plan.TargetRoot, TargetPath: plan.TargetPath, ContentHash: plan.ContentHash, Status: "applied", CreatedAt: time.Now().UTC()}
	if previous != nil {
		deployment.PreviousDeploymentID = previous.ID
		deployment.PreviousHash = previous.ContentHash
		deployment.Snapshot = snapshot
	}
	if err := writeDeployment(deploymentDir, deployment); err != nil {
		if deployedHash, hashErr := hashTree(plan.TargetPath); hashErr == nil && deployedHash == plan.ContentHash {
			_ = os.RemoveAll(plan.TargetPath)
			if quarantine != "" {
				if restoreErr := rename(quarantine, plan.TargetPath); restoreErr != nil {
					operation.Status = "partial"
					operation.RecoveryPath = quarantine
					removeState = false
					return Deployment{}, fmt.Errorf("registro falhou; versão anterior preservada em %s: %w", quarantine, restoreErr)
				}
			}
		} else {
			operation.Status = "partial"
			removeState = false
			return Deployment{}, fmt.Errorf("registro falhou e o destino mudou; estado incompleto preservado em %s: %w", deploymentDir, err)
		}
		return Deployment{}, err
	}
	operation.Status = "applied"
	if quarantine != "" {
		if err := os.RemoveAll(quarantine); err != nil {
			removeState = false
			return deployment, fmt.Errorf("deployment aplicado; limpeza do snapshot temporário falhou: %w", err)
		}
	}
	removeState = false
	keepTargetDirs = true
	return deployment, nil
}

// Rollback removes only an unchanged tree that has a matching AgentHub deployment record.
func Rollback(id, provider, allowedTargetRoot, stateRoot string) error {
	if !validID(id) {
		return fmt.Errorf("deployment id inválido")
	}
	stateRoot, err := filepath.Abs(stateRoot)
	if err != nil {
		return err
	}
	deploymentDir := filepath.Join(stateRoot, "deployments", id)
	b, err := os.ReadFile(filepath.Join(deploymentDir, "deployment.json"))
	if err != nil {
		return fmt.Errorf("deployment não encontrado: %w", err)
	}
	var deployment Deployment
	if err := json.Unmarshal(b, &deployment); err != nil {
		return fmt.Errorf("registro de deployment inválido: %w", err)
	}
	root, err := filepath.Abs(allowedTargetRoot)
	if err != nil {
		return err
	}
	if deployment.ID != id || deployment.Provider != provider || deployment.TargetRoot != root || deployment.TargetPath != filepath.Join(root, deployment.Slug) {
		return fmt.Errorf("deployment não corresponde ao provider/destino autorizado")
	}
	if deployment.Status == "removed" {
		return fmt.Errorf("deployment já removido; rollback não altera recursos removidos")
	}
	if !validSlug(deployment.Slug) {
		return fmt.Errorf("slug inválido no registro de deployment")
	}
	if err := rejectSymlinkPath(root); err != nil && !os.IsNotExist(err) {
		return err
	}
	if deployment.PreviousHash != "" {
		currentHash, err := hashTree(deployment.TargetPath)
		if err != nil || currentHash != deployment.ContentHash {
			return fmt.Errorf("destino mudou após update; rollback bloqueado")
		}
		backup := filepath.Join(stateRoot, filepath.FromSlash(deployment.Snapshot))
		rel, relErr := filepath.Rel(stateRoot, backup)
		if relErr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			h, err := hashTree(backup)
			if err != nil || h != deployment.PreviousHash {
				return fmt.Errorf("snapshot anterior ausente ou alterado")
			}
			if err := replaceTree(backup, deployment.TargetPath, root); err != nil {
				return err
			}
			if restoredHash, err := hashTree(deployment.TargetPath); err != nil || restoredHash != deployment.PreviousHash {
				return fmt.Errorf("hash restaurado não confere; snapshot preservado em %s", backup)
			}
		} else {
			return fmt.Errorf("caminho de snapshot inválido")
		}
	} else if _, err := os.Lstat(deployment.TargetPath); err == nil {
		quarantine, err := os.MkdirTemp(root, ".agenthub-rollback-")
		if err != nil {
			return err
		}
		if err := os.Remove(quarantine); err != nil {
			return err
		}
		if err := fsutil.RenameNoReplace(deployment.TargetPath, quarantine); err != nil {
			return err
		}
		hash, hashErr := hashTree(quarantine)
		if hashErr != nil || hash != deployment.ContentHash {
			if _, targetErr := os.Lstat(deployment.TargetPath); os.IsNotExist(targetErr) {
				if restoreErr := fsutil.RenameNoReplace(quarantine, deployment.TargetPath); restoreErr != nil {
					return fmt.Errorf("destino mudou; conteúdo preservado em %s; restauração falhou: %w", quarantine, restoreErr)
				}
			} else {
				return fmt.Errorf("destino mudou; conteúdo preservado em %s", quarantine)
			}
			if hashErr != nil {
				return hashErr
			}
			return fmt.Errorf("destino tem alterações externas; rollback preservado")
		}
		if err := os.RemoveAll(quarantine); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(deploymentDir)
}

func replaceTree(source, target, root string) error {
	stage, err := os.MkdirTemp(root, ".agenthub-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := copyTree(source, stage); err != nil {
		return err
	}
	quarantine, err := os.MkdirTemp(root, ".agenthub-rollback-")
	if err != nil {
		return err
	}
	if err := os.Remove(quarantine); err != nil {
		return err
	}
	if err := fsutil.RenameNoReplace(target, quarantine); err != nil {
		return err
	}
	if err := fsutil.RenameNoReplace(stage, target); err != nil {
		_ = fsutil.RenameNoReplace(quarantine, target)
		return err
	}
	return os.RemoveAll(quarantine)
}

func findDeploymentByID(stateRoot, id string) (Deployment, error) {
	if !validID(id) {
		return Deployment{}, fmt.Errorf("deployment id inválido no registry")
	}
	b, err := os.ReadFile(filepath.Join(stateRoot, "deployments", id, "deployment.json"))
	if err != nil {
		return Deployment{}, err
	}
	var d Deployment
	if err := json.Unmarshal(b, &d); err != nil || d.ID != id {
		return Deployment{}, fmt.Errorf("registro de deployment inválido")
	}
	return d, nil
}

func findDeploymentByPath(stateRoot, provider, scope, target string) (*Deployment, error) {
	return findDeployment(stateRoot, provider, scope, target, "")
}

func findDeploymentByHash(stateRoot, provider, scope, target, hash string) (*Deployment, error) {
	return findDeployment(stateRoot, provider, scope, target, hash)
}

func findDeployment(stateRoot, provider, scope, target, hash string) (*Deployment, error) {
	entries, err := ListDeployments(stateRoot, scope)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		d := entries[i]
		if d.Status == "applied" && d.Provider == provider && filepath.Clean(d.TargetPath) == filepath.Clean(target) && (hash == "" || d.ContentHash == hash) {
			return &d, nil
		}
	}
	return nil, nil
}

func validatePlan(plan SkillPlan, allowedLibraryRoot, allowedTargetRoot string) error {
	if plan.Version != 1 || plan.Kind != "skill" || !validSlug(plan.Slug) || plan.ResourceID == "" || plan.Scope != "global" && plan.Scope != "project" {
		return fmt.Errorf("plano inválido ou não suportado")
	}
	digest, err := planDigest(plan)
	if err != nil {
		return err
	}
	if digest != plan.PlanHash {
		return fmt.Errorf("integridade do plano inválida")
	}
	root, err := filepath.Abs(allowedTargetRoot)
	if err != nil {
		return err
	}
	planRoot, err := filepath.Abs(plan.TargetRoot)
	if err != nil {
		return err
	}
	if root != planRoot || filepath.Clean(plan.TargetPath) != filepath.Join(root, plan.Slug) {
		return fmt.Errorf("destino do plano não corresponde ao adapter autorizado")
	}
	libraryRoot, err := filepath.Abs(allowedLibraryRoot)
	if err != nil {
		return err
	}
	source, err := filepath.Abs(plan.SourceRoot)
	if err != nil {
		return err
	}
	if source != filepath.Join(libraryRoot, "skills", plan.Slug) {
		return fmt.Errorf("origem do plano não corresponde à biblioteca canônica autorizada")
	}
	if plan.Provider != "codex" && plan.Provider != "claude-code" && plan.Provider != "opencode" {
		return fmt.Errorf("provider do plano inválido")
	}
	return nil
}

func existingDeployment(dir string, plan SkillPlan, stateRoot string) (Deployment, error) {
	b, err := os.ReadFile(filepath.Join(dir, "deployment.json"))
	if err != nil {
		return Deployment{}, fmt.Errorf("operação anterior incompleta; inspecione o estado antes de repetir: %w", err)
	}
	var deployment Deployment
	if err := json.Unmarshal(b, &deployment); err != nil {
		return Deployment{}, err
	}
	if deployment.ID != plan.PlanHash || deployment.ContentHash != plan.ContentHash || deployment.TargetPath != plan.TargetPath {
		return Deployment{}, fmt.Errorf("registro de deployment diverge do plano")
	}
	hash, err := hashTree(plan.TargetPath)
	if err != nil || hash != deployment.ContentHash {
		return Deployment{}, fmt.Errorf("destino gerenciado divergiu; não reaplicar silenciosamente")
	}
	operation, err := ReadOperation(stateRoot, plan.PlanHash)
	if os.IsNotExist(err) {
		return deployment, nil
	}
	if err != nil {
		return Deployment{}, err
	}
	if operation.TargetPath != plan.TargetPath || operation.RevisionHash != plan.ContentHash {
		return Deployment{}, fmt.Errorf("journal de operação diverge do deployment")
	}
	if operation.Version == 1 {
		operation.Version = 2
		operation.ResourceID, operation.Provider, operation.Scope, operation.Slug = plan.ResourceID, plan.Provider, plan.Scope, plan.Slug
		operation.TargetRoot, operation.TargetAction, operation.PreviousDeploymentID = plan.TargetRoot, plan.TargetAction, plan.PreviousDeploymentID
		if plan.TargetAction == "update" {
			operation.Snapshot = filepath.ToSlash(filepath.Join("backups", "deployments", plan.PlanHash))
		}
	}
	if operation.Status == "applying" {
		operation.Status, operation.Error, operation.UpdatedAt = "applied", "", time.Now().UTC()
		if err := writeOperation(stateRoot, operation); err != nil {
			return Deployment{}, fmt.Errorf("deployment existe, mas a recuperação do journal falhou: %w", err)
		}
	} else if operation.Status != "applied" {
		return Deployment{}, fmt.Errorf("deployment existe com operação %s; inspeção manual necessária", operation.Status)
	}
	return deployment, nil
}

func writeDeployment(dir string, deployment Deployment) error {
	b, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "deployment.tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "deployment.json"))
}

func planDigest(plan SkillPlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func validSlug(slug string) bool {
	if slug == "" || slug == "." || slug == ".." || strings.TrimSpace(slug) != slug || strings.HasSuffix(slug, ".") {
		return false
	}
	if strings.ContainsAny(slug, `/\\<>:"|?*`) {
		return false
	}
	for _, r := range slug {
		if r < 32 {
			return false
		}
	}
	deviceName := strings.ToUpper(strings.SplitN(slug, ".", 2)[0])
	if deviceName == "CON" || deviceName == "PRN" || deviceName == "AUX" || deviceName == "NUL" {
		return false
	}
	for i := 1; i <= 9; i++ {
		if deviceName == fmt.Sprintf("COM%d", i) || deviceName == fmt.Sprintf("LPT%d", i) {
			return false
		}
	}
	return true
}

func validID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == sha256.Size
}

func validResourceID(id string) bool {
	if strings.HasPrefix(id, "skills:") {
		return validSlug(strings.TrimPrefix(id, "skills:"))
	}
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil && len(decoded) == 16
}

func hashTree(root string) (string, error) {
	h := sha256.New()
	var total int64
	files := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if fsutil.IsLinkOrJunction(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("tipo de arquivo não permitido: %s", path)
		}
		if info.IsDir() {
			return nil
		}
		files++
		total += info.Size()
		if files > 10000 || total > maxSkillBytes {
			return fmt.Errorf("skill excede limites de arquivos ou tamanho")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		io.WriteString(h, filepath.ToSlash(rel)+"\x00")
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func listFiles(root string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if fsutil.IsLinkOrJunction(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("tipo de arquivo não permitido: %s", path)
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files, err
}

func copyTree(src, dst string) error {
	total := int64(0)
	files := 0
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if fsutil.IsLinkOrJunction(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("tipo de arquivo não permitido: %s", path)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.Mkdir(target, info.Mode().Perm())
		}
		files++
		if files > 10000 || info.Size() > maxSkillBytes-total {
			return fmt.Errorf("skill excede limites de arquivos ou tamanho")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(out, io.LimitReader(in, maxSkillBytes-total+1))
		closeErr := out.Close()
		total += written
		if copyErr != nil {
			return copyErr
		}
		if written > maxSkillBytes-(total-written) {
			return fmt.Errorf("skill excede limite de tamanho")
		}
		return closeErr
	})
}

func rejectSymlinkPath(path string) error {
	return fsutil.RejectLinkPath(path)
}

func mkdirAllTracked(path string) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkPath(abs); err != nil {
		return nil, err
	}
	missing := []string{}
	for current := abs; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("destino não é diretório: %s", current)
			}
			break
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	created := []string{}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0700); err != nil {
			if !os.IsExist(err) {
				removeEmptyDirs(created)
				return nil, err
			}
			info, statErr := os.Lstat(missing[i])
			if statErr != nil || !info.IsDir() {
				removeEmptyDirs(created)
				return nil, fmt.Errorf("destino não é diretório: %s", missing[i])
			}
			continue
		}
		created = append(created, missing[i])
	}
	return created, nil
}

func removeEmptyDirs(paths []string) {
	for i := len(paths) - 1; i >= 0; i-- {
		_ = os.Remove(paths[i])
	}
}
