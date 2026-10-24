package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type MigrationPlan struct {
	Version       int                `json:"schema_version"`
	SourceRoot    string             `json:"source_root"`
	TargetRoot    string             `json:"target_root"`
	SourcePresent bool               `json:"source_present"`
	Items         []MigrationItem    `json:"items"`
	Metadata      *MigrationMetadata `json:"legacy_metadata,omitempty"`
	Warnings      []string           `json:"warnings,omitempty"`
	PlanHash      string             `json:"plan_hash"`
}

type MigrationItem struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Source     string   `json:"source_path"`
	Target     string   `json:"target_path"`
	SourceHash string   `json:"source_hash,omitempty"`
	TargetHash string   `json:"target_hash,omitempty"`
	Files      []string `json:"files,omitempty"`
	Status     string   `json:"status"`
	Reason     string   `json:"reason,omitempty"`
}

type MigrationMetadata struct {
	Source     string `json:"source_path"`
	Target     string `json:"target_path"`
	SourceHash string `json:"source_hash,omitempty"`
	TargetHash string `json:"target_hash,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
}

// PlanMigration inventories the old private library and legacy lock without writing either root.
func PlanMigration(sourceRoot, targetRoot, legacyLock string) (MigrationPlan, error) {
	var err error
	plan := MigrationPlan{Version: 1, Items: []MigrationItem{}, Warnings: []string{}}
	plan.SourceRoot, err = filepath.Abs(sourceRoot)
	if err != nil {
		return MigrationPlan{}, err
	}
	plan.TargetRoot, err = filepath.Abs(targetRoot)
	if err != nil {
		return MigrationPlan{}, err
	}
	if rootsOverlap(plan.SourceRoot, plan.TargetRoot) {
		return MigrationPlan{}, fmt.Errorf("raiz de origem e destino da migração não podem se sobrepor")
	}
	legacyLock, err = filepath.Abs(legacyLock)
	if err != nil {
		return MigrationPlan{}, err
	}
	if err := rejectSymlinkPath(plan.SourceRoot); err != nil {
		return MigrationPlan{}, err
	}
	if err := rejectSymlinkPath(plan.TargetRoot); err != nil {
		return MigrationPlan{}, err
	}
	if err := rejectSymlinkPath(registryPath(plan.TargetRoot)); err != nil {
		return MigrationPlan{}, err
	}
	registered, err := ListAll(plan.TargetRoot)
	if err != nil {
		return MigrationPlan{}, err
	}
	if err := rejectSymlinkPath(legacyLock); err != nil {
		return MigrationPlan{}, err
	}
	if info, err := os.Stat(plan.SourceRoot); err == nil {
		if !info.IsDir() {
			return MigrationPlan{}, fmt.Errorf("origem legada não é diretório")
		}
		plan.SourcePresent = true
	} else if !os.IsNotExist(err) {
		return MigrationPlan{}, err
	}
	if plan.SourcePresent {
		entries, err := os.ReadDir(plan.SourceRoot)
		if err != nil {
			return MigrationPlan{}, err
		}
		for _, entry := range entries {
			kind := entry.Name()
			if !entry.IsDir() || !validType(kind) {
				plan.Warnings = append(plan.Warnings, "entrada ignorada (tipo ou formato não reconhecido): "+filepath.Join(plan.SourceRoot, kind))
				continue
			}
			kindRoot := filepath.Join(plan.SourceRoot, kind)
			if err := rejectSymlinkPath(kindRoot); err != nil {
				return MigrationPlan{}, err
			}
			resources, err := os.ReadDir(kindRoot)
			if err != nil {
				return MigrationPlan{}, err
			}
			for _, resource := range resources {
				plan.Items = append(plan.Items, planMigrationItem(kind, resource.Name(), filepath.Join(kindRoot, resource.Name()), filepath.Join(plan.TargetRoot, kind, resource.Name()), registered))
			}
		}
	}
	if _, err := os.Lstat(legacyLock); err == nil {
		metadata := &MigrationMetadata{Source: legacyLock, Target: filepath.Join(plan.TargetRoot, ".agenthub", "registry.json"), Status: "manual_review", Reason: "schema legado não localizado; conteúdo não é interpretado nem convertido"}
		if err := rejectSymlinkPath(metadata.Target); err != nil {
			metadata.Status = "blocked"
			metadata.Reason = err.Error()
			plan.Metadata = metadata
			plan.PlanHash, err = migrationPlanDigest(plan)
			return plan, err
		}
		info, statErr := os.Lstat(legacyLock)
		if statErr != nil {
			return MigrationPlan{}, statErr
		}
		if !info.Mode().IsRegular() {
			metadata.Status = "blocked"
			metadata.Reason = "arquivo legado não é regular; conteúdo preservado"
		} else {
			metadata.SourceHash, err = hashTree(legacyLock)
		}
		if err != nil {
			return MigrationPlan{}, err
		}
		if info, statErr := os.Lstat(metadata.Target); statErr == nil {
			if info.IsDir() {
				metadata.Status = "conflict"
				metadata.Reason = "destino de metadados já existe"
			} else {
				metadata.TargetHash, err = hashTree(metadata.Target)
				if err != nil {
					return MigrationPlan{}, err
				}
				metadata.Status = "conflict"
				metadata.Reason = "destino de metadados já existe"
			}
		} else if !os.IsNotExist(statErr) {
			return MigrationPlan{}, statErr
		}
		plan.Metadata = metadata
	} else if !os.IsNotExist(err) {
		return MigrationPlan{}, err
	}
	sort.Slice(plan.Items, func(i, j int) bool {
		if plan.Items[i].Target == plan.Items[j].Target {
			return plan.Items[i].Source < plan.Items[j].Source
		}
		return plan.Items[i].Target < plan.Items[j].Target
	})
	sort.Strings(plan.Warnings)
	plan.PlanHash, err = migrationPlanDigest(plan)
	return plan, err
}

func planMigrationItem(kind, name, source, target string, registered []Entry) MigrationItem {
	item := MigrationItem{Kind: kind, Name: name, Source: source, Target: target, Status: "blocked"}
	var err error
	if err := rejectSymlinkPath(source); err != nil {
		item.Reason = err.Error()
		return item
	}
	if err := rejectSymlinkPath(target); err != nil {
		item.Reason = err.Error()
		return item
	}
	if !validPortableResourceName(name) {
		item.Reason = "nome legado não é portátil para o formato canônico"
		return item
	}
	item.Files, err = resourceFiles(source)
	if err != nil {
		item.Reason = err.Error()
		return item
	}
	item.SourceHash, err = resourceContentHash(source, kind)
	if err != nil {
		item.Reason = err.Error()
		return item
	}
	if info, err := os.Lstat(target); err == nil {
		if info.IsDir() {
			item.TargetHash, err = hashTree(target)
		} else {
			item.Reason = "destino existe e não é diretório de recurso"
			return item
		}
		if err != nil {
			item.Reason = err.Error()
			return item
		}
		if item.TargetHash == item.SourceHash {
			item.Status = "already_present"
			item.Reason = "destino contém o mesmo hash; nenhuma ação proposta"
		} else {
			item.Status = "conflict"
			item.Reason = "destino contém conteúdo diferente; preservado"
		}
		return item
	} else if !os.IsNotExist(err) {
		item.Reason = err.Error()
		return item
	}
	if siblings, err := os.ReadDir(filepath.Dir(target)); err == nil {
		for _, sibling := range siblings {
			if !strings.EqualFold(sibling.Name(), name) {
				continue
			}
			item.Status = "conflict"
			item.Target = filepath.Join(filepath.Dir(target), sibling.Name())
			item.Reason = "destino com diferença apenas de maiúsculas/minúsculas; preservado"
			return item
		}
	} else if !os.IsNotExist(err) {
		item.Reason = err.Error()
		return item
	}
	for _, entry := range registered {
		if entry.Type == kind && strings.EqualFold(entry.Name, name) {
			item.Status = "conflict"
			item.Reason = "nome já registrado no destino; estado canônico preservado"
			return item
		}
	}
	item.Status = "ready"
	item.Reason = "origem regular e destino ausente; esta prévia não copia conteúdo"
	return item
}

func rootsOverlap(a, b string) bool {
	relAB, errAB := filepath.Rel(a, b)
	relBA, errBA := filepath.Rel(b, a)
	inside := func(rel string, err error) bool {
		return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
	}
	return inside(relAB, errAB) || inside(relBA, errBA)
}

func migrationPlanDigest(plan MigrationPlan) (string, error) {
	plan.PlanHash = ""
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
