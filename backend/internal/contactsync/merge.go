package contactsync

import (
	"reflect"
	"sort"
)

// Conflict captures an unresolved same-field disagreement between updates.
type Conflict struct {
	Field  string
	Base   FieldState
	Local  FieldState
	Remote FieldState
}

// MergeResult is the merged record plus any unresolved conflicts.
type MergeResult struct {
	Record    Record
	Conflicts []Conflict
}

// ResolveRecord merges a base record with local and remote versions using a
// field-level last-write-wins policy.
func ResolveRecord(base, local, remote Record) MergeResult {
	merged := Record{
		ExternalID: pickExternalID(base.ExternalID, local.ExternalID, remote.ExternalID),
		Tombstone:  resolveTombstone(base.Tombstone, local.Tombstone, remote.Tombstone),
		Fields:     make(map[string]FieldState),
	}

	fieldNames := make(map[string]struct{}, len(base.Fields)+len(local.Fields)+len(remote.Fields))
	for name := range base.Fields {
		fieldNames[name] = struct{}{}
	}
	for name := range local.Fields {
		fieldNames[name] = struct{}{}
	}
	for name := range remote.Fields {
		fieldNames[name] = struct{}{}
	}

	names := make([]string, 0, len(fieldNames))
	for name := range fieldNames {
		names = append(names, name)
	}
	sort.Strings(names)

	conflicts := make([]Conflict, 0)
	for _, name := range names {
		resolved, conflict := resolveField(name, base.Fields[name], local.Fields[name], remote.Fields[name])
		if conflict != nil {
			conflicts = append(conflicts, *conflict)
		}
		merged.Fields[name] = resolved
	}

	return MergeResult{Record: merged, Conflicts: conflicts}
}

func pickExternalID(base, local, remote string) string {
	switch {
	case local != "" && local == remote:
		return local
	case local != "" && remote == "":
		return local
	case remote != "" && local == "":
		return remote
	case local != "" && remote != "":
		return local
	case base != "":
		return base
	default:
		return ""
	}
}

func resolveTombstone(base, local, remote Tombstone) Tombstone {
	switch {
	case local.Deleted == remote.Deleted:
		if local.UpdatedAt.After(remote.UpdatedAt) {
			return local
		}
		return remote
	case sameState(base.Deleted, local.Deleted) && !sameState(base.Deleted, remote.Deleted):
		return remote
	case sameState(base.Deleted, remote.Deleted) && !sameState(base.Deleted, local.Deleted):
		return local
	case local.UpdatedAt.After(remote.UpdatedAt):
		return local
	case remote.UpdatedAt.After(local.UpdatedAt):
		return remote
	default:
		return local
	}
}

func resolveField(name string, base, local, remote FieldState) (FieldState, *Conflict) {
	if sameField(base, local) && sameField(base, remote) {
		return base, nil
	}
	if sameField(base, local) {
		return remote, nil
	}
	if sameField(base, remote) {
		return local, nil
	}
	if sameField(local, remote) {
		if local.UpdatedAt.After(remote.UpdatedAt) {
			return local, nil
		}
		return remote, nil
	}
	if local.UpdatedAt.After(remote.UpdatedAt) {
		return local, nil
	}
	if remote.UpdatedAt.After(local.UpdatedAt) {
		return remote, nil
	}
	return local, &Conflict{Field: name, Base: base, Local: local, Remote: remote}
}

func sameState(left, right bool) bool {
	return left == right
}

func sameField(left, right FieldState) bool {
	if left.IsSet != right.IsSet {
		return false
	}
	if !left.IsSet && !right.IsSet {
		return true
	}
	return reflect.DeepEqual(left.Value, right.Value)
}
