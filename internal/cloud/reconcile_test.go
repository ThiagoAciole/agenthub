package cloud

import "testing"

func TestReconcileThreeWayResourceChanges(t *testing.T) {
	base := map[string]Revision{"a": {ID: "a1", Hash: "base"}, "b": {ID: "b1", Hash: "base"}, "c": {ID: "c1", Hash: "base"}}
	local := map[string]Revision{"a": {ID: "a2", Hash: "local"}, "b": {ID: "b1", Hash: "base"}, "c": {ID: "c2", Hash: "same"}}
	remote := map[string]Revision{"a": {ID: "a3", Hash: "remote"}, "b": {ID: "b2", Hash: "remote"}, "c": {ID: "c3", Hash: "same"}}
	decisions, err := Reconcile(base, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 3 || decisions[0].ResourceID != "a" || decisions[0].Action != Conflict || decisions[0].Base.ID != "a1" || decisions[0].Local.ID != "a2" || decisions[0].Remote.ID != "a3" {
		t.Fatalf("divergent edits were not preserved as conflict: %+v", decisions)
	}
	if decisions[1].Action != Materialize || decisions[1].Chosen.ID != "b2" {
		t.Fatalf("remote-only change was not selected for materialization: %+v", decisions[1])
	}
	if decisions[2].Action != Converged || decisions[2].Chosen.Hash != "same" {
		t.Fatalf("equal independent changes were not converged: %+v", decisions[2])
	}
}

func TestReconcilePublishesLocalAndPreservesTombstones(t *testing.T) {
	base := map[string]Revision{"deleted": {ID: "d1", Hash: "old"}, "local": {ID: "l1", Hash: "old"}}
	local := map[string]Revision{"deleted": {ID: "d2", Tombstone: true}, "local": {ID: "l2", Hash: "new"}}
	remote := map[string]Revision{"deleted": {ID: "d1", Hash: "old"}, "local": {ID: "l1", Hash: "old"}}
	decisions, err := Reconcile(base, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if decisions[0].ResourceID != "deleted" || decisions[0].Action != Publish || decisions[0].Chosen == nil || !decisions[0].Chosen.Tombstone {
		t.Fatalf("deletion was not carried as an explicit tombstone: %+v", decisions[0])
	}
	if decisions[1].Action != Publish || decisions[1].Chosen.Hash != "new" {
		t.Fatalf("local-only change was not selected for publication: %+v", decisions[1])
	}
}

func TestReconcileRejectsInvalidRevision(t *testing.T) {
	if _, err := Reconcile(nil, map[string]Revision{"skill": {ID: "r1"}}, nil); err == nil {
		t.Fatal("accepted a live revision without content hash")
	}
}

func TestReconcileDoesNotTreatMissingTrackedResourceAsDeletion(t *testing.T) {
	base := map[string]Revision{"skill": {ID: "r1", Hash: "base"}}
	remote := map[string]Revision{"skill": {ID: "r1", Hash: "base"}}
	decisions, err := Reconcile(base, nil, remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Action != Conflict || decisions[0].Chosen != nil {
		t.Fatalf("missing tracked resource was treated as a deletion: %+v", decisions)
	}
}
