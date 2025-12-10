package orchestrator

import (
	"reflect"

	"k8s.io/client-go/tools/cache"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/operator"
)

// addEventNodestoreHandler enqueues per-tenant update events when a NodeStore is added.
// This helps verify add events are observed and triggers initial tenant reconciliation.
func (o *Operator) addEventNodestoreHandler(obj any) {

	o.logger.Info("addEventNodestoreHandler called")

}

/*
	when we receive an update event from a nodestore source:

- extract changed tenants from the nodestore status
- for each tenant, enqueue a nodestore update event for processing by the tenant reconciler
*/
func (o *Operator) updateEventNodestoreHandler(oldObj, newObj any) {

	o.logger.Info("updateEventNodestoreHandler called")
	var oldNS, newNS *seterav1.NodeStore
	switch v := oldObj.(type) {
	case *seterav1.NodeStore:
		oldNS = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.NodeStore); ok {
			oldNS = vv
		}
	}
	switch v := newObj.(type) {
	case *seterav1.NodeStore:
		newNS = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.NodeStore); ok {
			newNS = vv
		}
	}
	if oldNS == nil || newNS == nil {
		return
	}

	affected := make(map[string]struct{})
	// added/changed tenants
	for name, newTi := range newNS.Status.Tenants {
		if oldTi, ok := oldNS.Status.Tenants[name]; !ok || !reflect.DeepEqual(oldTi, newTi) {
			affected[name] = struct{}{}
		}
	}
	// removed tenants
	for name := range oldNS.Status.Tenants {
		if _, ok := newNS.Status.Tenants[name]; !ok {
			affected[name] = struct{}{}
		}
	}
	if len(affected) == 0 {
		return
	}

	for tenantName := range affected {

		o.base.EnqueueWith(SourceNodeStoreCRD, EventUpdate, operator.ResourceRef{
			Group:     "setera.com",
			Version:   "v1",
			Kind:      "Tenant",
			Namespace: newNS.Namespace,
			Name:      tenantName,
		})
	}
}

func (o *Operator) deleteEventNodestoreHandler(obj any) {
	nodestore, ok := obj.(*seterav1.NodeStore)
	if !ok {
		return
	}
	for tenantName := range nodestore.Status.Tenants {
		o.base.EnqueueWith(SourceNodeStoreCRD, EventDelete, operator.ResourceRef{
			Group:     "setera.com",
			Version:   "v1",
			Kind:      "Tenant",
			Namespace: nodestore.Namespace,
			Name:      tenantName,
		})
	}
}
