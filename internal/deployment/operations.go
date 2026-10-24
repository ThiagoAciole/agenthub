package deployment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Operation is the durable, per-plan record used to reconcile interrupted deployments.
type Operation struct {
	Version              int       `json:"schema_version"`
	ID                   string    `json:"operation_id"`
	IdempotencyKey       string    `json:"idempotency_key"`
	ResourceID           string    `json:"resource_id,omitempty"`
	Provider             string    `json:"provider_id,omitempty"`
	Scope                string    `json:"scope,omitempty"`
	Slug                 string    `json:"slug,omitempty"`
	TargetRoot           string    `json:"target_root,omitempty"`
	TargetPath           string    `json:"target_path"`
	TargetAction         string    `json:"target_action,omitempty"`
	BaseHash             string    `json:"base_hash,omitempty"`
	RevisionHash         string    `json:"revision_hash"`
	PreviousDeploymentID string    `json:"previous_deployment_id,omitempty"`
	Snapshot             string    `json:"snapshot,omitempty"`
	RecoveryPath         string    `json:"recovery_path,omitempty"`
	RecoveryPlanHash     string    `json:"recovery_plan_hash,omitempty"`
	Status               string    `json:"status"`
	Error                string    `json:"error,omitempty"`
	Attempts             int       `json:"attempts"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// ReadOperation loads and validates one journal entry by its plan-derived ID.
func ReadOperation(stateRoot, id string) (Operation, error) {
	if !validID(id) {
		return Operation{}, fmt.Errorf("operation ID inválido")
	}
	path := filepath.Join(stateRoot, "operations", id+".json")
	if err := rejectSymlinkPath(path); err != nil {
		return Operation{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Operation{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return Operation{}, fmt.Errorf("journal deve ser arquivo regular de até 1 MiB")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Operation{}, err
	}
	var operation Operation
	if err := json.Unmarshal(b, &operation); err != nil {
		return Operation{}, fmt.Errorf("journal de operação inválido: %w", err)
	}
	if operation.Version != 1 && operation.Version != 2 || operation.ID != id || operation.IdempotencyKey != id || operation.TargetPath == "" || operation.RevisionHash == "" || operation.Attempts < 1 {
		return Operation{}, fmt.Errorf("journal de operação inconsistente")
	}
	if operation.Version == 2 && !validOperation(operation) {
		return Operation{}, fmt.Errorf("metadados do journal de operação inválidos")
	}
	switch operation.Status {
	case "applying", "applied", "failed", "partial", "recovered":
		return operation, nil
	default:
		return Operation{}, fmt.Errorf("status de operação inválido: %s", operation.Status)
	}
}

// ListOperations returns the persisted journals, newest first.
func ListOperations(stateRoot string) ([]Operation, error) {
	root, err := filepath.Abs(stateRoot)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "operations")
	if err := rejectSymlinkPath(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Operation{}, nil
	}
	if err != nil {
		return nil, err
	}
	operations := make([]Operation, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !validID(id) {
			continue
		}
		operation, err := ReadOperation(root, id)
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].UpdatedAt.After(operations[j].UpdatedAt) })
	return operations, nil
}

func writeOperation(stateRoot string, operation Operation) error {
	if !validOperation(operation) {
		return fmt.Errorf("journal de operação inválido")
	}
	if err := rejectSymlinkPath(stateRoot); err != nil {
		return err
	}
	dir := filepath.Join(stateRoot, "operations")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := rejectSymlinkPath(dir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(operation, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".operation-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Join(dir, operation.ID+".json"))
}

func startOperation(stateRoot string, plan SkillPlan) (Operation, error) {
	operation, err := ReadOperation(stateRoot, plan.PlanHash)
	if err == nil {
		if operation.TargetPath != plan.TargetPath || operation.BaseHash != plan.TargetHash || operation.RevisionHash != plan.ContentHash {
			return Operation{}, fmt.Errorf("journal de operação diverge do plano")
		}
		switch operation.Status {
		case "applied":
			return Operation{}, fmt.Errorf("operação marcada applied sem deployment registrado")
		case "partial":
			return Operation{}, fmt.Errorf("operação parcial exige recuperação manual antes de novo apply")
		}
		operation.Attempts++
	} else if !os.IsNotExist(err) {
		return Operation{}, err
	} else {
		operation = Operation{ID: plan.PlanHash, IdempotencyKey: plan.PlanHash, Attempts: 1}
	}
	operation.Version = 2
	operation.ResourceID = plan.ResourceID
	operation.Provider = plan.Provider
	operation.Scope = plan.Scope
	operation.Slug = plan.Slug
	operation.TargetRoot = plan.TargetRoot
	operation.TargetPath = plan.TargetPath
	operation.TargetAction = plan.TargetAction
	operation.BaseHash = plan.TargetHash
	operation.RevisionHash = plan.ContentHash
	operation.PreviousDeploymentID = plan.PreviousDeploymentID
	operation.Snapshot = ""
	operation.RecoveryPath = ""
	operation.RecoveryPlanHash = ""
	if plan.TargetAction == "update" {
		operation.Snapshot = filepath.ToSlash(filepath.Join("backups", "deployments", plan.PlanHash))
	}
	operation.Status, operation.Error, operation.UpdatedAt = "applying", "", time.Now().UTC()
	if err := writeOperation(stateRoot, operation); err != nil {
		return Operation{}, err
	}
	return operation, nil
}

func validOperation(operation Operation) bool {
	if operation.Version != 2 || !validID(operation.ID) || operation.IdempotencyKey != operation.ID || !validResourceID(operation.ResourceID) || operation.Provider != "codex" && operation.Provider != "claude-code" && operation.Provider != "opencode" || operation.Scope != "global" && operation.Scope != "project" || !validSlug(operation.Slug) || !filepath.IsAbs(operation.TargetRoot) || operation.TargetPath != filepath.Join(operation.TargetRoot, operation.Slug) || operation.TargetAction != "create" && operation.TargetAction != "update" || !validID(operation.RevisionHash) || operation.BaseHash != "" && !validID(operation.BaseHash) || operation.Attempts < 1 {
		return false
	}
	if operation.PreviousDeploymentID != "" && !validID(operation.PreviousDeploymentID) {
		return false
	}
	if operation.RecoveryPlanHash != "" && !validID(operation.RecoveryPlanHash) {
		return false
	}
	if operation.Status != "applying" && operation.Status != "applied" && operation.Status != "failed" && operation.Status != "partial" && operation.Status != "recovered" {
		return false
	}
	return true
}
