package orchestrator

import (
	// k8s
	"k8s.io/client-go/tools/cache"

	// internal pkg
	seterav1 "github/setera/pkg/api/setera.com/v1"
)

// add tenant key to the workqueue - add event
func (t *Operator) addEventTenantHandler(obj any) {
	tenant, ok := obj.(*seterav1.Tenant)
	if !ok {
		t.logger.WithValues("event", EventAdd).Info("failed to cast object to tenant in add handler")
		return
	}
	t.logger.WithValues("event", EventAdd, "tenant", tenant.Name, "ns", tenant.Namespace).Info("enqueue tenant add")
	t.base.EnqueueObjectWith(SourceTenantCRD, EventAdd, tenant)
}

// add tenant key to the workqueue - update event (spec changes only)
func (t *Operator) updateEventTenantHandler(oldObj, newObj any) {
	// Handle tombstones defensively
	var oldTenant, newTenant *seterav1.Tenant
	switch v := oldObj.(type) {

	case *seterav1.Tenant:
		oldTenant = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			oldTenant = vv
		}
	}

	switch v := newObj.(type) {

	case *seterav1.Tenant:
		newTenant = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			newTenant = vv
		}
	}
	if oldTenant == nil || newTenant == nil {
		t.logger.WithValues("event", EventUpdate).Info("failed to cast objects to tenant in update handler")
		return
	}

	// 1) Spec change? Use Generation (status/metadata updates do not bump it).
	if oldTenant.Generation != newTenant.Generation {
		t.logger.WithValues("event", EventUpdate, "tenant", newTenant.Name, "ns", newTenant.Namespace).Info("enqueue tenant update (spec change)")
		t.base.EnqueueObjectWith(SourceTenantCRD, EventUpdate, newTenant)
		return
	}

}

// add tenant key to the workqueue - deletion event
func (t *Operator) deleteEventTenantHandler(obj any) {
	var tenant *seterav1.Tenant
	switch v := obj.(type) {
	case *seterav1.Tenant:
		tenant = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			tenant = vv
		}
	}
	if tenant == nil {
		t.logger.WithValues("event", EventDelete).Info("failed to cast object to tenant in delete handler")
		return
	}

	t.logger.WithValues("event", EventDelete, "tenant", tenant.Name, "ns", tenant.Namespace).Info("enqueue tenant delete")
	t.base.EnqueueObjectWith(SourceTenantCRD, EventDelete, tenant)
}

// add tenants key in deleted nodestore for processing
func (t *Operator) deleteFromNodestoreHandler(obj any) {

	nodestore, ok := obj.(*seterav1.NodeStore)
	logger := t.logger.WithValues("event", EventDelete, "nodestore", nodestore.Name)
	if !ok {
		logger.Info("Failed to cast object to nodestore in delete handler")
		return
	}

	logger.Info("Deleting nodestore")
	//o.enqueue(nodestore, DeleteEvent)

}
