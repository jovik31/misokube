package orchestrator

import (

	// internal imports
	"fmt"
	seterav1 "github/setera/pkg/api/setera.com/v1"

	"k8s.io/client-go/tools/cache"
)

const indexNodeStoreByTenant = "setera.com/tenant"

// returns tenants names assigned to the nodestore
func indexNodestoreByTenant(obj any) ([]string, error) {

	ns, ok := obj.(*seterav1.NodeStore)
	if !ok || ns == nil || ns.Status.Tenants == nil {

		return nil, fmt.Errorf("object is not a nodestore: %d", obj)
	}

	out := make([]string, 0, len(ns.Status.Tenants))
	for _, t := range ns.Status.Tenants {
		out = append(out, t.Name)
	}
	return out, nil

}

func (o *Operator) addNodeStoreIndexes() error {

	if err := o.nodeStoreInf.AddIndexers(
		cache.Indexers{
			indexNodeStoreByTenant: indexNodestoreByTenant,
		},
	); err != nil {
		return fmt.Errorf("add NodeStore informer indexes: %w", err)
	}
	return nil
}
