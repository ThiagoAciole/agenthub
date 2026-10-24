package deployment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ThiagoAciole/agenthub/internal/fsutil"
)

type DeploymentRemovalPlan struct {
	Version      int      `json:"schema_version"`
	DeploymentID string   `json:"deployment_id"`
	ResourceID   string   `json:"resource_id"`
	Provider     string   `json:"provider_id"`
	Scope        string   `json:"scope"`
	TargetRoot   string   `json:"target_root"`
	TargetPath   string   `json:"target_path"`
	ContentHash  string   `json:"content_hash"`
	Files        []string `json:"files_to_remove"`
	Risk         string   `json:"risk"`
	PlanHash     string   `json:"plan_hash"`
}

func ListDeployments(stateRoot, scope string) ([]Deployment, error) {
	if scope != "all" && scope != "global" && scope != "project" {
		return nil, fmt.Errorf("escopo inválido: %s", scope)
	}
	stateRoot, err := filepath.Abs(stateRoot)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkPath(stateRoot); err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(filepath.Join(stateRoot, "deployments"))
	if os.IsNotExist(err) {
		return []Deployment{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]Deployment, 0, len(dirs))
	for _, dir := range dirs {
		if !dir.IsDir() || !validID(dir.Name()) {
			continue
		}
		deployment, err := findDeploymentByID(stateRoot, dir.Name())
		if err != nil {
			return nil, err
		}
		if scope == "all" || deployment.Scope == scope {
			result = append(result, deployment)
		}
	}
	superseded := make(map[string]bool)
	for _, deployment := range result {
		if deployment.PreviousDeploymentID != "" && (deployment.Status == "applied" || deployment.Status == "removed") {
			superseded[deployment.PreviousDeploymentID] = true
		}
	}
	for i := range result {
		if result[i].Status == "applied" && superseded[result[i].ID] {
			result[i].Status = "superseded"
		}
	}
	return result, nil
}

func PlanRemoveDeployment(id, provider, scope, targetRoot, stateRoot string) (DeploymentRemovalPlan, error) {
	if !validID(id) || provider != "codex" && provider != "claude-code" && provider != "opencode" || scope != "global" && scope != "project" {
		return DeploymentRemovalPlan{}, fmt.Errorf("deployment, provider ou escopo inválido")
	}
	targetRoot, err := filepath.Abs(targetRoot)
	if err != nil {
		return DeploymentRemovalPlan{}, err
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return DeploymentRemovalPlan{}, err
	}
	deployment, err := findDeploymentByID(stateRoot, id)
	if err != nil {
		return DeploymentRemovalPlan{}, fmt.Errorf("deployment não encontrado: %w", err)
	}
	if deployment.Status != "applied" {
		return DeploymentRemovalPlan{}, fmt.Errorf("deployment já removido")
	}
	listed, err := ListDeployments(stateRoot, scope)
	if err != nil {
		return DeploymentRemovalPlan{}, err
	}
	current := false
	for _, item := range listed {
		if item.ID == id && item.Status == "applied" {
			current = true
			break
		}
	}
	if !current {
		return DeploymentRemovalPlan{}, fmt.Errorf("deployment superseded; apenas o deployment ativo pode ser removido")
	}
	if deployment.Provider != provider || deployment.Scope != scope || deployment.TargetRoot != targetRoot || deployment.TargetPath != filepath.Join(targetRoot, deployment.Slug) {
		return DeploymentRemovalPlan{}, fmt.Errorf("deployment não corresponde ao provider/destino autorizado")
	}
	if err := rejectSymlinkPath(targetRoot); err != nil && !os.IsNotExist(err) {
		return DeploymentRemovalPlan{}, err
	}
	if err := rejectSymlinkPath(deployment.TargetPath); err != nil {
		return DeploymentRemovalPlan{}, err
	}
	hash, err := hashTree(deployment.TargetPath)
	if err != nil {
		return DeploymentRemovalPlan{}, err
	}
	if hash != deployment.ContentHash {
		return DeploymentRemovalPlan{}, fmt.Errorf("destino tem drift; remoção bloqueada")
	}
	files, err := listFiles(deployment.TargetPath)
	if err != nil {
		return DeploymentRemovalPlan{}, err
	}
	plan := DeploymentRemovalPlan{Version: 1, DeploymentID: id, ResourceID: deployment.ResourceID, Provider: provider, Scope: scope, TargetRoot: targetRoot, TargetPath: deployment.TargetPath, ContentHash: hash, Files: files, Risk: "Remove somente o bundle gerenciado; salva snapshot privado e preserva o registro histórico."}
	plan.PlanHash, err = deploymentRemovalDigest(plan)
	return plan, err
}

func ApplyRemoveDeployment(plan DeploymentRemovalPlan, allowedTargetRoot, stateRoot string) (Deployment, error) {
	if plan.Version != 1 || !validID(plan.DeploymentID) || plan.Provider != "codex" && plan.Provider != "claude-code" && plan.Provider != "opencode" || plan.Scope != "global" && plan.Scope != "project" {
		return Deployment{}, fmt.Errorf("versão de plano não suportada")
	}
	digest, err := deploymentRemovalDigest(plan)
	if err != nil {
		return Deployment{}, err
	}
	if digest != plan.PlanHash {
		return Deployment{}, fmt.Errorf("integridade do plano de remoção inválida")
	}
	root, err := filepath.Abs(allowedTargetRoot)
	if err != nil {
		return Deployment{}, err
	}
	if root != plan.TargetRoot || plan.TargetPath != filepath.Join(root, filepath.Base(plan.TargetPath)) || !validSlug(filepath.Base(plan.TargetPath)) {
		return Deployment{}, fmt.Errorf("destino do plano não corresponde ao provider autorizado")
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return Deployment{}, err
	}
	if err := rejectSymlinkPath(stateRoot); err != nil {
		return Deployment{}, err
	}
	expectedSnapshot := filepath.ToSlash(filepath.Join("backups", "deployments", "removals", plan.PlanHash))
	deployment, err := findDeploymentByID(stateRoot, plan.DeploymentID)
	if err != nil {
		return Deployment{}, err
	}
	if deployment.ID != plan.DeploymentID || deployment.ResourceID != plan.ResourceID || deployment.Provider != plan.Provider || deployment.Scope != plan.Scope || deployment.TargetRoot != root || deployment.TargetPath != plan.TargetPath || deployment.ContentHash != plan.ContentHash {
		return Deployment{}, fmt.Errorf("registro de deployment diverge do plano")
	}
	if deployment.Status == "removed" && deployment.LastOperationID == plan.PlanHash {
		if deployment.RemovalSnapshot != expectedSnapshot {
			return Deployment{}, fmt.Errorf("snapshot do deployment removido tem caminho inválido")
		}
		backup := filepath.Join(stateRoot, filepath.FromSlash(deployment.RemovalSnapshot))
		if h, err := hashTree(backup); err == nil && h == plan.ContentHash {
			if _, err := os.Lstat(plan.TargetPath); os.IsNotExist(err) {
				return deployment, nil
			}
		}
		return Deployment{}, fmt.Errorf("estado do deployment removido mudou")
	}
	listed, err := ListDeployments(stateRoot, plan.Scope)
	if err != nil {
		return Deployment{}, err
	}
	current := false
	for _, item := range listed {
		if item.ID == plan.DeploymentID && item.Status == "applied" {
			current = true
			break
		}
	}
	if !current {
		return Deployment{}, fmt.Errorf("deployment superseded após plan")
	}
	if deployment.Status == "removed" || deployment.ResourceID != plan.ResourceID || deployment.Provider != plan.Provider || deployment.Scope != plan.Scope || deployment.TargetRoot != root || deployment.TargetPath != plan.TargetPath || deployment.ContentHash != plan.ContentHash {
		return Deployment{}, fmt.Errorf("ownership mudou após plan")
	}
	if err := rejectSymlinkPath(plan.TargetPath); err != nil {
		return Deployment{}, err
	}
	hash, err := hashTree(plan.TargetPath)
	if err != nil || hash != plan.ContentHash {
		return Deployment{}, fmt.Errorf("destino mudou após plan; remoção bloqueada")
	}
	files, err := listFiles(plan.TargetPath)
	if err != nil {
		return Deployment{}, err
	}
	if len(files) != len(plan.Files) {
		return Deployment{}, fmt.Errorf("lista de arquivos diverge do plano")
	}
	for i := range files {
		if files[i] != plan.Files[i] {
			return Deployment{}, fmt.Errorf("lista de arquivos diverge do plano")
		}
	}
	backupRel := expectedSnapshot
	backup := filepath.Join(stateRoot, filepath.FromSlash(backupRel))
	if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
		return Deployment{}, err
	}
	if _, err := os.Lstat(backup); err == nil {
		if h, err := hashTree(backup); err != nil || h != plan.ContentHash {
			return Deployment{}, fmt.Errorf("snapshot existente diverge")
		}
	} else if !os.IsNotExist(err) {
		return Deployment{}, err
	} else {
		stage, err := os.MkdirTemp(filepath.Dir(backup), ".snapshot-")
		if err != nil {
			return Deployment{}, err
		}
		defer os.RemoveAll(stage)
		if err := copyTree(plan.TargetPath, stage); err != nil {
			return Deployment{}, err
		}
		if h, err := hashTree(stage); err != nil || h != plan.ContentHash {
			return Deployment{}, fmt.Errorf("hash do snapshot não confere")
		}
		if err := fsutil.RenameNoReplace(stage, backup); err != nil {
			return Deployment{}, err
		}
	}
	if h, err := hashTree(plan.TargetPath); err != nil || h != plan.ContentHash {
		return Deployment{}, fmt.Errorf("destino mudou antes da remoção")
	}
	quarantine, err := os.MkdirTemp(root, ".agenthub-remove-")
	if err != nil {
		return Deployment{}, err
	}
	if err := os.Remove(quarantine); err != nil {
		return Deployment{}, err
	}
	if err := fsutil.RenameNoReplace(plan.TargetPath, quarantine); err != nil {
		return Deployment{}, err
	}
	if h, err := hashTree(quarantine); err != nil || h != plan.ContentHash {
		if restoreErr := fsutil.RenameNoReplace(quarantine, plan.TargetPath); restoreErr != nil {
			return Deployment{}, fmt.Errorf("destino mudou; conteúdo preservado em %s: %w", quarantine, restoreErr)
		}
		return Deployment{}, fmt.Errorf("destino mudou durante remoção")
	}
	now := time.Now().UTC()
	deployment.Status, deployment.RemovalSnapshot, deployment.LastOperationID, deployment.RemovedAt = "removed", backupRel, plan.PlanHash, &now
	if err := writeDeployment(filepath.Join(stateRoot, "deployments", deployment.ID), deployment); err != nil {
		if restoreErr := fsutil.RenameNoReplace(quarantine, plan.TargetPath); restoreErr != nil {
			return Deployment{}, fmt.Errorf("registro falhou; bundle preservado em %s: %w", quarantine, restoreErr)
		}
		return Deployment{}, err
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return deployment, fmt.Errorf("deployment removido; limpeza da quarentena falhou: %w", err)
	}
	return deployment, nil
}

func deploymentRemovalDigest(plan DeploymentRemovalPlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func findRemovedDeploymentByPath(stateRoot, provider, scope, target string) (*Deployment, error) {
	deployments, err := ListDeployments(stateRoot, scope)
	if err != nil {
		return nil, err
	}
	var latest *Deployment
	for i := range deployments {
		d := deployments[i]
		if d.Status != "removed" || d.Provider != provider || filepath.Clean(d.TargetPath) != filepath.Clean(target) {
			continue
		}
		if latest == nil || latest.RemovedAt == nil || d.RemovedAt != nil && d.RemovedAt.After(*latest.RemovedAt) {
			copy := d
			latest = &copy
		}
	}
	return latest, nil
}
