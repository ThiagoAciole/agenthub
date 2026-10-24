package cloud

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ThiagoAciole/agenthub/internal/library"
)

// ApplyRemoteDecision applies one approved materialize-remote decision after revalidating both sides.
func ApplyRemoteDecision(root, stateRoot, candidatePath string, plan SyncPlan, resourceID string) (library.Entry, error) {
	if err := VerifySyncPlan(plan); err != nil {
		return library.Entry{}, err
	}
	var decision *SyncDecision
	for i := range plan.Decisions {
		if plan.Decisions[i].ResourceID == resourceID {
			if decision != nil {
				return library.Entry{}, fmt.Errorf("resourceId duplicado no plano")
			}
			decision = &plan.Decisions[i]
		}
	}
	if decision == nil || decision.Action != Materialize || decision.Remote == nil {
		return library.Entry{}, fmt.Errorf("plano não aprova materialização remota para %s", resourceID)
	}
	if decision.Chosen == nil || !sameSnapshotState(*decision.Chosen, *decision.Remote) {
		return library.Entry{}, fmt.Errorf("versão escolhida diverge do snapshot remoto")
	}
	remoteSnapshot := Snapshot{Version: 1, Resources: make([]SnapshotResource, 0)}
	for _, item := range plan.Decisions {
		if item.Remote != nil {
			remoteSnapshot.Resources = append(remoteSnapshot.Resources, *item.Remote)
		}
	}
	if err := validateSnapshot(candidatePath, remoteSnapshot); err != nil {
		return library.Entry{}, fmt.Errorf("candidato remoto mudou ou ficou inválido: %w", err)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return library.Entry{}, err
	}
	stateRoot, err = filepath.Abs(stateRoot)
	if err != nil {
		return library.Entry{}, err
	}
	candidatePath, err = filepath.Abs(candidatePath)
	if err != nil {
		return library.Entry{}, err
	}
	if pathsOverlap(root, stateRoot) || pathsOverlap(root, candidatePath) {
		return library.Entry{}, fmt.Errorf("biblioteca, estado e candidato remoto devem ter raízes isoladas")
	}
	entries, err := library.ListAll(root)
	if err != nil {
		return library.Entry{}, err
	}
	var current *library.Entry
	for i := range entries {
		if entries[i].ID == resourceID {
			if current != nil {
				return library.Entry{}, fmt.Errorf("resourceId duplicado no registry local")
			}
			copy := entries[i]
			current = &copy
		}
	}
	remote := *decision.Remote
	if current != nil && sameSyncState(snapshotFromEntry(*current), remote) {
		if remote.Tombstone {
			if _, err := os.Lstat(filepath.Join(root, remote.Type, remote.Name)); os.IsNotExist(err) {
				return *current, nil
			} else if err != nil {
				return library.Entry{}, err
			}
		} else {
			currentPlan, err := library.PlanUpdate(root, remote.Name, filepath.Join(candidatePath, remote.Type, remote.Name), remote.Type)
			if err == nil && currentPlan.AfterHash == remote.Hash {
				return *current, nil
			}
		}
	}
	if decision.Local == nil {
		if current != nil {
			return library.Entry{}, fmt.Errorf("registry local mudou após o plano; gere nova prévia")
		}
	} else {
		if current == nil || !sameSnapshotState(snapshotFromEntry(*current), *decision.Local) {
			return library.Entry{}, fmt.Errorf("estado local mudou após o plano; gere nova prévia")
		}
	}
	if remote.Tombstone {
		if current == nil {
			return library.AddSyncedTombstone(root, remote.ResourceID, remote.Type, remote.Name, remote.RevisionID, remote.Hash)
		}
		if current.Status == "tombstoned" || current.Type != remote.Type || current.Name != remote.Name || current.SHA256 != remote.Hash {
			return library.Entry{}, fmt.Errorf("tombstone remoto exige mesmo path e conteúdo local; resolução manual necessária")
		}
		removePlan, err := library.PlanRemove(root, current.Name, current.Type)
		if err != nil {
			return library.Entry{}, err
		}
		if removePlan.ResourceID != resourceID || removePlan.ContentHash != remote.Hash {
			return library.Entry{}, fmt.Errorf("conteúdo mudou após o plano de sincronização")
		}
		return library.ApplyRemove(removePlan, root, stateRoot)
	}
	if current != nil && (current.Status == "tombstoned" || current.Type != remote.Type || current.Name != remote.Name) {
		return library.Entry{}, fmt.Errorf("rename ou restauração remota ainda exige resolução própria")
	}
	source := filepath.Join(candidatePath, remote.Type, remote.Name)
	if current == nil {
		addPlan, err := library.PlanAddLocal(root, source, remote.Type, remote.Name)
		if err != nil {
			return library.Entry{}, err
		}
		if addPlan.ContentHash != remote.Hash {
			return library.Entry{}, fmt.Errorf("hash do recurso remoto diverge do plano")
		}
		return library.ApplyAddRevision(addPlan, root, remote.ResourceID, remote.RevisionID)
	}
	updatePlan, err := library.PlanUpdate(root, current.Name, source, current.Type)
	if err != nil {
		return library.Entry{}, err
	}
	if updatePlan.ResourceID != resourceID || updatePlan.AfterHash != remote.Hash {
		return library.Entry{}, fmt.Errorf("conteúdo local/remoto diverge do plano aprovado")
	}
	return library.ApplyUpdateRevision(updatePlan, root, stateRoot, decision.Local.RevisionID, remote.RevisionID, "cloud")
}

func snapshotFromEntry(entry library.Entry) SnapshotResource {
	revision := library.CurrentRevision(entry)
	return SnapshotResource{
		ResourceID: entry.ID,
		Type:       entry.Type,
		Name:       entry.Name,
		RevisionID: revision.ID,
		Hash:       entry.SHA256,
		Tombstone:  entry.Status == "tombstoned",
	}
}

func sameSnapshotState(a, b SnapshotResource) bool {
	return a.ResourceID == b.ResourceID && a.Type == b.Type && a.Name == b.Name && a.RevisionID == b.RevisionID && a.Hash == b.Hash && a.Tombstone == b.Tombstone
}

func sameSyncState(a, b SnapshotResource) bool {
	return a.ResourceID == b.ResourceID && a.Type == b.Type && a.Name == b.Name && a.Hash == b.Hash && a.Tombstone == b.Tombstone
}
