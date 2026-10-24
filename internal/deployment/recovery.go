package deployment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThiagoAciole/agenthub/internal/fsutil"
)

type OperationRecoveryPlan struct {
	Version      int    `json:"schema_version"`
	OperationID  string `json:"operation_id"`
	StateRoot    string `json:"state_root"`
	TargetRoot   string `json:"target_root"`
	TargetPath   string `json:"target_path"`
	Action       string `json:"action"`
	CurrentHash  string `json:"current_hash,omitempty"`
	RestoreFrom  string `json:"restore_from,omitempty"`
	ExpectedHash string `json:"expected_hash,omitempty"`
	Risk         string `json:"risk"`
	PlanHash     string `json:"plan_hash"`
}

// PlanOperationRecovery describes a safe recovery from the hashes recorded in an operation.
func PlanOperationRecovery(stateRoot, id, allowedTargetRoot string) (OperationRecoveryPlan, error) {
	stateRoot, err := filepath.Abs(stateRoot)
	if err != nil {
		return OperationRecoveryPlan{}, err
	}
	if err := rejectSymlinkPath(stateRoot); err != nil {
		return OperationRecoveryPlan{}, err
	}
	operation, err := ReadOperation(stateRoot, id)
	if err != nil {
		return OperationRecoveryPlan{}, err
	}
	if operation.Status != "partial" && operation.Status != "applying" {
		return OperationRecoveryPlan{}, fmt.Errorf("operação %s não exige recuperação: %s", id, operation.Status)
	}
	if operation.Version != 2 || operation.ResourceID == "" || operation.Provider == "" || operation.Scope == "" || operation.Slug == "" || operation.TargetRoot == "" || operation.TargetAction == "" || operation.TargetPath != filepath.Join(operation.TargetRoot, operation.Slug) {
		return OperationRecoveryPlan{}, fmt.Errorf("journal legado/incompleto não contém dados suficientes para recovery assistido")
	}
	allowedTargetRoot, err = filepath.Abs(allowedTargetRoot)
	if err != nil {
		return OperationRecoveryPlan{}, err
	}
	if operation.TargetRoot != allowedTargetRoot {
		return OperationRecoveryPlan{}, fmt.Errorf("journal aponta para destino fora do root autorizado")
	}
	if operation.Provider != "codex" && operation.Provider != "claude-code" && operation.Provider != "opencode" || operation.Scope != "global" && operation.Scope != "project" || !validSlug(operation.Slug) {
		return OperationRecoveryPlan{}, fmt.Errorf("metadados de destino inválidos no journal")
	}
	if err := rejectSymlinkPath(operation.TargetRoot); err != nil && !os.IsNotExist(err) {
		return OperationRecoveryPlan{}, err
	}
	currentHash, exists, err := hashIfPresent(operation.TargetPath)
	if err != nil {
		return OperationRecoveryPlan{}, err
	}
	plan := OperationRecoveryPlan{Version: 1, OperationID: id, StateRoot: stateRoot, TargetRoot: operation.TargetRoot, TargetPath: operation.TargetPath, CurrentHash: currentHash}
	switch {
	case exists && currentHash == operation.RevisionHash:
		if operation.BaseHash != "" {
			if _, err := verifiedSnapshot(stateRoot, operation); err != nil {
				return OperationRecoveryPlan{}, fmt.Errorf("não é seguro finalizar update sem snapshot íntegro: %w", err)
			}
		}
		plan.Action = "finalize"
		plan.ExpectedHash = operation.RevisionHash
		plan.Risk = "Registra o deployment existente após confirmar o hash instalado e o snapshot anterior."
	case exists && operation.BaseHash != "" && currentHash == operation.BaseHash:
		plan.Action = "mark_recovered"
		plan.ExpectedHash = operation.BaseHash
		plan.Risk = "Confirma que o destino continua na revisão anterior; não altera conteúdo do provider."
	case !exists && operation.BaseHash == "":
		plan.Action = "mark_recovered"
		plan.Risk = "Confirma que o apply de criação não deixou destino; não altera conteúdo do provider."
	case !exists && operation.BaseHash != "":
		source, err := verifiedRecoverySource(stateRoot, operation)
		if err != nil {
			return OperationRecoveryPlan{}, err
		}
		plan.Action = "restore_base"
		plan.RestoreFrom = source
		plan.ExpectedHash = operation.BaseHash
		plan.Risk = "Restaura o snapshot privado ou a quarentena somente no destino ausente e após validar hashes."
	default:
		return OperationRecoveryPlan{}, fmt.Errorf("destino diverge das revisões do journal; preservado para resolução manual")
	}
	plan.PlanHash, err = recoveryPlanDigest(plan)
	return plan, err
}

// ApplyOperationRecovery applies only the exact recovery plan after rechecking its state.
func ApplyOperationRecovery(plan OperationRecoveryPlan, allowedTargetRoot, stateRoot string) (Operation, error) {
	if plan.Version != 1 || !validID(plan.OperationID) {
		return Operation{}, fmt.Errorf("versão ou operation ID do recovery inválidos")
	}
	digest, err := recoveryPlanDigest(plan)
	if err != nil || digest != plan.PlanHash {
		return Operation{}, fmt.Errorf("integridade do recovery plan inválida")
	}
	root, err := filepath.Abs(stateRoot)
	if err != nil {
		return Operation{}, err
	}
	if root != plan.StateRoot {
		return Operation{}, fmt.Errorf("recovery plan pertence a outro state root")
	}
	targetRoot, err := filepath.Abs(allowedTargetRoot)
	if err != nil {
		return Operation{}, err
	}
	if targetRoot != plan.TargetRoot {
		return Operation{}, fmt.Errorf("recovery plan pertence a outro root de destino")
	}
	operation, err := ReadOperation(root, plan.OperationID)
	if err != nil {
		return Operation{}, err
	}
	if operation.RecoveryPlanHash == plan.PlanHash && (operation.Status == "applied" || operation.Status == "recovered") {
		if err := verifyRecoveryPostcondition(root, operation, plan); err != nil {
			return Operation{}, err
		}
		return operation, nil
	}
	current, err := PlanOperationRecovery(root, plan.OperationID, targetRoot)
	if err != nil {
		return Operation{}, err
	}
	if current.PlanHash != plan.PlanHash {
		return Operation{}, fmt.Errorf("estado mudou após recovery plan; gere nova prévia")
	}
	switch plan.Action {
	case "finalize":
		if err := finalizeRecoveredDeployment(root, operation); err != nil {
			return Operation{}, err
		}
		operation.Status = "applied"
	case "mark_recovered":
		if err := removeEmptyRecoveryDeploymentDir(root, operation.ID); err != nil {
			return Operation{}, err
		}
		operation.Status = "recovered"
	case "restore_base":
		if err := restoreRecoveryBase(operation, plan); err != nil {
			return Operation{}, err
		}
		if err := removeEmptyRecoveryDeploymentDir(root, operation.ID); err != nil {
			return Operation{}, err
		}
		operation.Status = "recovered"
	default:
		return Operation{}, fmt.Errorf("ação de recovery inválida")
	}
	operation.RecoveryPlanHash = plan.PlanHash
	operation.Error = ""
	operation.UpdatedAt = time.Now().UTC()
	if err := writeOperation(root, operation); err != nil {
		return Operation{}, fmt.Errorf("recovery aplicado; journal não atualizado: %w", err)
	}
	return operation, nil
}

func restoreRecoveryBase(operation Operation, plan OperationRecoveryPlan) error {
	if plan.RestoreFrom == "" || plan.ExpectedHash != operation.BaseHash {
		return fmt.Errorf("plano de restauração não corresponde à revisão-base")
	}
	if hash, exists, err := hashIfPresent(operation.TargetPath); err != nil || exists {
		if err != nil {
			return err
		}
		return fmt.Errorf("destino surgiu após recovery plan (%s); preservado", hash)
	}
	if err := rejectSymlinkPath(plan.RestoreFrom); err != nil {
		return err
	}
	if hash, err := hashTree(plan.RestoreFrom); err != nil || hash != operation.BaseHash {
		return fmt.Errorf("fonte de recovery mudou após prévia")
	}
	createdDirs, err := mkdirAllTracked(operation.TargetRoot)
	if err != nil {
		return err
	}
	keepTargetRoot := false
	defer func() {
		if !keepTargetRoot {
			removeEmptyDirs(createdDirs)
		}
	}()
	stage, err := os.MkdirTemp(operation.TargetRoot, ".agenthub-recovery-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := copyTree(plan.RestoreFrom, stage); err != nil {
		return err
	}
	if hash, err := hashTree(stage); err != nil || hash != operation.BaseHash {
		return fmt.Errorf("hash do stage de recovery não confere")
	}
	if _, exists, err := hashIfPresent(operation.TargetPath); err != nil || exists {
		if err != nil {
			return err
		}
		return fmt.Errorf("destino surgiu durante recovery; preservado")
	}
	if err := fsutil.RenameNoReplace(stage, operation.TargetPath); err != nil {
		return err
	}
	if hash, err := hashTree(operation.TargetPath); err != nil || hash != operation.BaseHash {
		return fmt.Errorf("hash restaurado não confere; destino preservado")
	}
	keepTargetRoot = true
	return nil
}

func finalizeRecoveredDeployment(stateRoot string, operation Operation) error {
	if hash, err := hashTree(operation.TargetPath); err != nil || hash != operation.RevisionHash {
		return fmt.Errorf("destino não corresponde à revisão a finalizar")
	}
	if operation.PreviousDeploymentID != "" {
		previous, err := findDeploymentByID(stateRoot, operation.PreviousDeploymentID)
		if err != nil || previous.Status != "applied" || previous.ResourceID != operation.ResourceID || previous.Provider != operation.Provider || previous.Scope != operation.Scope || previous.TargetPath != operation.TargetPath || previous.ContentHash != operation.BaseHash {
			return fmt.Errorf("deployment anterior não confere com o journal")
		}
		if _, err := verifiedSnapshot(stateRoot, operation); err != nil {
			return err
		}
	}
	deployment := Deployment{ID: operation.ID, ResourceID: operation.ResourceID, Provider: operation.Provider, Scope: operation.Scope, Slug: operation.Slug, TargetRoot: operation.TargetRoot, TargetPath: operation.TargetPath, ContentHash: operation.RevisionHash, Status: "applied", CreatedAt: time.Now().UTC(), PreviousDeploymentID: operation.PreviousDeploymentID, Snapshot: operation.Snapshot}
	if operation.PreviousDeploymentID != "" {
		deployment.PreviousHash = operation.BaseHash
	}
	dir := filepath.Join(stateRoot, "deployments", operation.ID)
	if err := rejectSymlinkPath(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "deployment.json")
	if _, err := os.Lstat(path); err == nil {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var existing Deployment
		if err := json.Unmarshal(b, &existing); err != nil || existing.ID != deployment.ID || existing.ResourceID != deployment.ResourceID || existing.Provider != deployment.Provider || existing.Scope != deployment.Scope || existing.Slug != deployment.Slug || existing.TargetRoot != deployment.TargetRoot || existing.ContentHash != deployment.ContentHash || existing.TargetPath != deployment.TargetPath || existing.Status != "applied" || existing.PreviousDeploymentID != deployment.PreviousDeploymentID || existing.PreviousHash != deployment.PreviousHash || existing.Snapshot != deployment.Snapshot {
			return fmt.Errorf("deployment existente diverge do recovery")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("diretório de deployment contém estado desconhecido; preservado")
	}
	return writeDeployment(dir, deployment)
}

func verifyRecoveryPostcondition(stateRoot string, operation Operation, plan OperationRecoveryPlan) error {
	switch plan.Action {
	case "finalize":
		if operation.Status != "applied" {
			return fmt.Errorf("estado final de recovery diverge")
		}
		return finalizeRecoveredDeployment(stateRoot, operation)
	case "restore_base":
		hash, err := hashTree(operation.TargetPath)
		if err != nil || hash != operation.BaseHash || operation.Status != "recovered" {
			return fmt.Errorf("recovery-base não está mais no estado registrado")
		}
	case "mark_recovered":
		if operation.Status != "recovered" {
			return fmt.Errorf("estado final de recovery diverge")
		}
		if operation.BaseHash == "" {
			if _, err := os.Lstat(operation.TargetPath); !os.IsNotExist(err) {
				return fmt.Errorf("destino surgiu após recovery")
			}
		} else if hash, err := hashTree(operation.TargetPath); err != nil || hash != operation.BaseHash {
			return fmt.Errorf("destino mudou após recovery")
		}
	default:
		return fmt.Errorf("ação de recovery inválida")
	}
	return nil
}

func removeEmptyRecoveryDeploymentDir(stateRoot string, id string) error {
	dir := filepath.Join(stateRoot, "deployments", id)
	if err := rejectSymlinkPath(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("diretório de deployment contém estado incompleto; preservado para inspeção")
	}
	return os.Remove(dir)
}

func verifiedRecoverySource(stateRoot string, operation Operation) (string, error) {
	if operation.RecoveryPath != "" {
		path, err := filepath.Abs(operation.RecoveryPath)
		if err != nil || filepath.Dir(path) != operation.TargetRoot || !strings.HasPrefix(filepath.Base(path), ".agenthub-previous-") {
			return "", fmt.Errorf("caminho de quarentena inválido no journal")
		}
		if err := rejectSymlinkPath(path); err == nil {
			if hash, hashErr := hashTree(path); hashErr == nil && hash == operation.BaseHash {
				return path, nil
			}
		}
	}
	return verifiedSnapshot(stateRoot, operation)
}

func verifiedSnapshot(stateRoot string, operation Operation) (string, error) {
	want := filepath.ToSlash(filepath.Join("backups", "deployments", operation.ID))
	if operation.Snapshot != want || !filepath.IsLocal(filepath.FromSlash(operation.Snapshot)) {
		return "", fmt.Errorf("snapshot privado inválido no journal")
	}
	path := filepath.Join(stateRoot, filepath.FromSlash(operation.Snapshot))
	if err := rejectSymlinkPath(path); err != nil {
		return "", err
	}
	hash, err := hashTree(path)
	if err != nil || hash != operation.BaseHash {
		return "", fmt.Errorf("snapshot ausente ou com hash divergente")
	}
	return path, nil
}

func hashIfPresent(path string) (string, bool, error) {
	if err := rejectSymlinkPath(path); err != nil {
		return "", false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("destino de recovery não é diretório: %s", path)
	}
	hash, err := hashTree(path)
	return hash, true, err
}

func recoveryPlanDigest(plan OperationRecoveryPlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}
