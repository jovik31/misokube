package orchestrator

import (
	// internal imports
	seterav1 "github/setera/pkg/api/setera.com/v1"
)

const indexNodeStoreByTenant = "setera.com/tenant"

// returns tenants names assigned to the nodestore
func indexNodestoreByTenant(obj any) ([]string, error) {

	ns, ok := obj.(*seterav1.NodeStore)
	if !ok || ns == nil || ns.Status.Tenants == nil {
		// Return no keys rather than error to keep indexers robust
		return []string{}, nil
	}

	out := make([]string, 0, len(ns.Status.Tenants))
	for _, t := range ns.Status.Tenants {
		if t.Name != "" {
			out = append(out, t.Name)
		}
	}
	if len(out) == 0 {
		// No tenants present; do not index this NodeStore
		return []string{}, nil
	}
	return out, nil

}
