package cloud

import "testing"

func syncResource(id, revision, name, hash string) SnapshotResource {
	return SnapshotResource{ResourceID: id, Type: "skills", Name: name, RevisionID: revision, Hash: hash}
}

func TestPlanSyncTreatsRenameAsStateAndPreservesConflictVersions(t *testing.T) {
	base := Snapshot{Version: 1, Resources: []SnapshotResource{syncResource("resource-1", "r1", "old-name", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}}
	local := Snapshot{Version: 1, Resources: []SnapshotResource{syncResource("resource-1", "r2", "local-name", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}}
	remote := Snapshot{Version: 1, Resources: []SnapshotResource{syncResource("resource-1", "r3", "remote-name", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}}
	plan, err := PlanSync(base, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Decisions) != 1 {
		t.Fatalf("unexpected decision count: %+v", plan.Decisions)
	}
	decision := plan.Decisions[0]
	if decision.Action != Conflict || decision.Base.Name != "old-name" || decision.Local.Name != "local-name" || decision.Remote.Name != "remote-name" || decision.Chosen != nil {
		t.Fatalf("concurrent renames were not preserved as a conflict: %+v", decision)
	}
	if err := VerifySyncPlan(plan); err != nil {
		t.Fatalf("generated plan failed integrity check: %v", err)
	}
}

func TestPlanSyncPublishesOneSidedRenameAndDetectsRemoteChange(t *testing.T) {
	hash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	base := Snapshot{Version: 1, Resources: []SnapshotResource{syncResource("resource-2", "r1", "old-name", hash)}}
	local := Snapshot{Version: 1, Resources: []SnapshotResource{syncResource("resource-2", "r2", "new-name", hash)}}
	remote := Snapshot{Version: 1, Resources: []SnapshotResource{syncResource("resource-2", "r1", "old-name", hash)}}
	plan, err := PlanSync(base, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decisions[0].Action != Publish || plan.Decisions[0].Chosen.Name != "new-name" {
		t.Fatalf("one-sided rename was lost: %+v", plan.Decisions[0])
	}
	remote = Snapshot{Version: 1, Resources: []SnapshotResource{syncResource("resource-2", "r3", "old-name", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")}}
	plan, err = PlanSync(base, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decisions[0].Action != Conflict || plan.Decisions[0].Base.Hash != hash || plan.Decisions[0].Local.Name != "new-name" || plan.Decisions[0].Remote.Hash == hash {
		t.Fatalf("content/rename conflict lost a revision: %+v", plan.Decisions[0])
	}
}

func TestPlanSyncRejectsInvalidSnapshotsAndTamperedPlan(t *testing.T) {
	empty := Snapshot{Version: 1}
	duplicate := Snapshot{Version: 1, Resources: []SnapshotResource{
		syncResource("same", "r1", "one", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
		syncResource("same", "r2", "two", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
	}}
	if _, err := PlanSync(empty, duplicate, empty); err == nil {
		t.Fatal("accepted duplicate resourceId in a sync snapshot")
	}
	local := Snapshot{Version: 1, Resources: []SnapshotResource{syncResource("resource", "r1", "skill", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")}}
	plan, err := PlanSync(empty, local, empty)
	if err != nil {
		t.Fatal(err)
	}
	plan.Decisions[0].Chosen.Name = "tampered"
	if err := VerifySyncPlan(plan); err == nil {
		t.Fatal("accepted a modified sync plan")
	}
}

func TestReconcileTreatsEqualContentWithDifferentNamesAsRename(t *testing.T) {
	base := map[string]Revision{"id": {ID: "r1", Hash: "same", Type: "skills", Name: "old"}}
	local := map[string]Revision{"id": {ID: "r2", Hash: "same", Type: "skills", Name: "new"}}
	remote := map[string]Revision{"id": {ID: "r1", Hash: "same", Type: "skills", Name: "old"}}
	decisions, err := Reconcile(base, local, remote)
	if err != nil || len(decisions) != 1 || decisions[0].Action != Publish {
		t.Fatalf("metadata-only rename was ignored: %+v %v", decisions, err)
	}
}
