package cloud

import (
	"fmt"
	"sort"
)

type Revision struct {
	ID        string `json:"revision_id"`
	Hash      string `json:"content_hash,omitempty"`
	Tombstone bool   `json:"tombstone,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
}

type Action string

const (
	Noop        Action = "noop"
	Publish     Action = "publish-local"
	Materialize Action = "materialize-remote"
	Converged   Action = "converged"
	Conflict    Action = "conflict"
)

type Decision struct {
	ResourceID string    `json:"resource_id"`
	Action     Action    `json:"action"`
	Base       *Revision `json:"base,omitempty"`
	Local      *Revision `json:"local,omitempty"`
	Remote     *Revision `json:"remote,omitempty"`
	Chosen     *Revision `json:"chosen,omitempty"`
}

// Reconcile compares snapshot state by content, tombstone and canonical path, not timestamps.
func Reconcile(base, local, remote map[string]Revision) ([]Decision, error) {
	ids := make(map[string]struct{}, len(base)+len(local)+len(remote))
	for _, side := range []map[string]Revision{base, local, remote} {
		for id, revision := range side {
			if id == "" || revision.ID == "" || (!revision.Tombstone && revision.Hash == "") {
				return nil, fmt.Errorf("revisão inválida para resource %q", id)
			}
			ids[id] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	decisions := make([]Decision, 0, len(ordered))
	for _, id := range ordered {
		b, bok := base[id]
		l, lok := local[id]
		r, rok := remote[id]
		d := Decision{ResourceID: id, Base: revisionPtr(b, bok), Local: revisionPtr(l, lok), Remote: revisionPtr(r, rok)}
		switch {
		case bok && (!lok || !rok):
			d.Action = Conflict
		case equal(l, lok, r, rok):
			if equal(l, lok, b, bok) {
				d.Action = Noop
			} else {
				d.Action = Converged
			}
			d.Chosen = revisionPtr(l, lok)
		case equal(l, lok, b, bok):
			d.Action, d.Chosen = Materialize, revisionPtr(r, rok)
		case equal(r, rok, b, bok):
			d.Action, d.Chosen = Publish, revisionPtr(l, lok)
		case lok && rok && sameState(l, r):
			d.Action, d.Chosen = Converged, revisionPtr(l, true)
		default:
			d.Action = Conflict
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}

func revisionPtr(revision Revision, ok bool) *Revision {
	if !ok {
		return nil
	}
	copy := revision
	return &copy
}

func equal(a Revision, aok bool, b Revision, bok bool) bool {
	return aok == bok && (!aok || sameState(a, b))
}

func sameState(a, b Revision) bool {
	return a.Tombstone == b.Tombstone && a.Hash == b.Hash && a.Type == b.Type && a.Name == b.Name
}
