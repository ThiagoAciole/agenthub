package cloud

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type SyncPlan struct {
	Version   int            `json:"schema_version"`
	Decisions []SyncDecision `json:"decisions"`
	PlanHash  string         `json:"plan_hash"`
}

type SyncDecision struct {
	ResourceID string            `json:"resource_id"`
	Action     Action            `json:"action"`
	Base       *SnapshotResource `json:"base,omitempty"`
	Local      *SnapshotResource `json:"local,omitempty"`
	Remote     *SnapshotResource `json:"remote,omitempty"`
	Chosen     *SnapshotResource `json:"chosen,omitempty"`
}

// PlanSync combines validated snapshot metadata using the resource three-way decision rules.
func PlanSync(base, local, remote Snapshot) (SyncPlan, error) {
	baseResources, baseRevisions, err := snapshotResources(base, "base")
	if err != nil {
		return SyncPlan{}, err
	}
	localResources, localRevisions, err := snapshotResources(local, "local")
	if err != nil {
		return SyncPlan{}, err
	}
	remoteResources, remoteRevisions, err := snapshotResources(remote, "remote")
	if err != nil {
		return SyncPlan{}, err
	}
	resourceTypes := make(map[string]string)
	for _, side := range []map[string]SnapshotResource{baseResources, localResources, remoteResources} {
		for id, resource := range side {
			if previous, exists := resourceTypes[id]; exists && previous != resource.Type {
				return SyncPlan{}, fmt.Errorf("resourceId %s mudou de tipo entre snapshots", id)
			}
			resourceTypes[id] = resource.Type
		}
	}
	decisions, err := Reconcile(baseRevisions, localRevisions, remoteRevisions)
	if err != nil {
		return SyncPlan{}, err
	}
	plan := SyncPlan{Version: 1, Decisions: make([]SyncDecision, 0, len(decisions))}
	for _, decision := range decisions {
		item := SyncDecision{ResourceID: decision.ResourceID, Action: decision.Action}
		item.Base = snapshotResourcePtr(baseResources, decision.ResourceID)
		item.Local = snapshotResourcePtr(localResources, decision.ResourceID)
		item.Remote = snapshotResourcePtr(remoteResources, decision.ResourceID)
		switch decision.Action {
		case Publish, Converged, Noop:
			item.Chosen = firstResource(item.Local, item.Remote, item.Base)
		case Materialize:
			item.Chosen = firstResource(item.Remote, item.Local, item.Base)
		}
		plan.Decisions = append(plan.Decisions, item)
	}
	plan.PlanHash, err = syncPlanDigest(plan)
	if err != nil {
		return SyncPlan{}, err
	}
	return plan, nil
}

func VerifySyncPlan(plan SyncPlan) error {
	if plan.Version != 1 {
		return fmt.Errorf("versão do plano sync não suportada")
	}
	digest, err := syncPlanDigest(plan)
	if err != nil {
		return err
	}
	if digest != plan.PlanHash {
		return fmt.Errorf("integridade do plano sync inválida")
	}
	return nil
}

func snapshotResources(snapshot Snapshot, side string) (map[string]SnapshotResource, map[string]Revision, error) {
	if snapshot.Version != 1 || len(snapshot.Resources) > maxSnapshotResources {
		return nil, nil, fmt.Errorf("snapshot %s inválido", side)
	}
	resources := make(map[string]SnapshotResource, len(snapshot.Resources))
	revisions := make(map[string]Revision, len(snapshot.Resources))
	paths := make(map[string]string, len(snapshot.Resources))
	for _, resource := range snapshot.Resources {
		if resource.ResourceID == "" || resource.RevisionID == "" || !validSyncType(resource.Type) || !validSyncName(resource.Name) || (!resource.Tombstone && (len(resource.Hash) != 64 || !isHexString(resource.Hash))) || (resource.Hash != "" && (len(resource.Hash) != 64 || !isHexString(resource.Hash))) {
			return nil, nil, fmt.Errorf("recurso inválido no snapshot %s", side)
		}
		if _, exists := resources[resource.ResourceID]; exists {
			return nil, nil, fmt.Errorf("resourceId duplicado no snapshot %s: %s", side, resource.ResourceID)
		}
		path := strings.ToLower(resource.Type + "/" + resource.Name)
		if previous, exists := paths[path]; exists {
			return nil, nil, fmt.Errorf("colisão de paths no snapshot %s: %s e %s", side, previous, resource.ResourceID)
		}
		paths[path] = resource.ResourceID
		resources[resource.ResourceID] = resource
		revisions[resource.ResourceID] = Revision{ID: resource.RevisionID, Hash: resource.Hash, Tombstone: resource.Tombstone, Type: resource.Type, Name: resource.Name}
	}
	return resources, revisions, nil
}

func snapshotResourcePtr(resources map[string]SnapshotResource, id string) *SnapshotResource {
	resource, ok := resources[id]
	if !ok {
		return nil
	}
	copy := resource
	return &copy
}

func firstResource(resources ...*SnapshotResource) *SnapshotResource {
	for _, resource := range resources {
		if resource != nil {
			return resource
		}
	}
	return nil
}

func syncPlanDigest(plan SyncPlan) (string, error) {
	plan.PlanHash = ""
	plan.Decisions = append([]SyncDecision(nil), plan.Decisions...)
	sort.Slice(plan.Decisions, func(i, j int) bool { return plan.Decisions[i].ResourceID < plan.Decisions[j].ResourceID })
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}
