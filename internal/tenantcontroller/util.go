package tenantcontroller

import (
	seterav1 "github/setera/pkg/api/setera.com/v1"
)

// containsStr returns true if v is present in xs.
func containsStr(xs []string, v string) bool {
	for _, s := range xs {
		if s == v {
			return true
		}
	}
	return false
}

// sameStrSlice compares two string slices for exact order/content equality.
func sameStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalNodeInfosByName compares NodeInfo slices by node identity (Name only).
func equalNodeInfosByName(a, b []seterav1.NodeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]struct{}, len(a))
	for _, x := range a {
		m[x.Name] = struct{}{}
	}
	for _, y := range b {
		if _, ok := m[y.Name]; !ok {
			return false
		}
	}
	return true
}

// equalNodeInfosByValue compares NodeInfo slices by full value keyed by Name.
func equalNodeInfosByValue(a, b []seterav1.NodeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]seterav1.NodeInfo, len(a))
	for _, x := range a {
		m[x.Name] = x
	}
	for _, y := range b {
		if x, ok := m[y.Name]; !ok || x != y {
			return false
		}
	}
	return true
}
