package orchestrator

import (
	// internal pkg
	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/operator"
)

// add tenant key to the workqueue - add event
func (t *TenantOperator) addTenantHandler(obj any) {

	tenant, ok := obj.(*seterav1.Tenant)
	logger := t.Base.Logger.WithValues("event", operator.AddEvent, "tenant", tenant.Name)

	if !ok {
		logger.Info("Failed to cast object to tenant in add handler")
		return

	}

	t.Base.Enqueue(tenant, operator.AddEvent)

}

// add tenant key to the workqueue - update event
func (t *TenantOperator) updateTenantHandler(oldObj, newObj any) {

	oldTenant, ok := oldObj.(*seterav1.Tenant)
	logger := t.Base.Logger.WithValues("event", operator.UpdateEvent, "tenant", oldTenant.Name)
	if !ok {
		logger.Info("failed to cast old object to tenant in update handler")
		return
	}

	newTenant, ok := newObj.(*seterav1.Tenant)
	if !ok {
		logger.Info("Failed to cast new object to tenant in update handler")
		return
	}

	if oldTenant.Generation == newTenant.Generation {
		return
	}

	// Only add update event for tenants where the number of nodes has changed
	if oldTenant.Spec.Zones != newTenant.Spec.Zones {

		//logger.Info("Number of nodes changed in tenant,Add tenant to queue - Update event ")
		//logger.Info("Add tenant to queue - Update event")
		//o.enqueue(newTenant, UpdateEvent)

	}
}

// add tenant key to the workqueue - deletion event
func (t *TenantOperator) deleteTenantHandler(obj any) {

	tenant, ok := obj.(*seterav1.Tenant)
	logger := t.Base.Logger.WithValues("event", operator.UpdateEvent, "tenant", tenant.Name)
	if !ok {
		logger.Info("Failed to cast object to tenant in delete handler")
		return
	}

	logger.Info("Deleting tenant")
	//o.enqueue(tenant, DeleteEvent)

}

// Wrong implementation
func (t *TenantOperator) updateTenantFromNodestoreHandler(oldObj, newObj any) {

	oldNodestore, ok := oldObj.(*seterav1.NodeStore)
	logger := t.Base.Logger.WithValues("event", operator.UpdateEvent, "nodestore", oldNodestore.Name)
	if !ok {
		logger.Info("Failed to cast old object to nodestore in update handler")
		return
	}

	newNodestore, ok := newObj.(*seterav1.NodeStore)
	if !ok {
		logger.Info("Failed to cast new object to nodestore in update handler")
		return
	}

	if oldNodestore.ResourceVersion == newNodestore.ResourceVersion {
		return
	}

	logger.Info("Updating nodestore")
	//o.enqueue(newNodestore, UpdateEvent)

}

// add tenants key in deleted nodestore for processing
func (t *TenantOperator) deleteTenantFromNodestoreHandler(obj any) {

	nodestore, ok := obj.(*seterav1.NodeStore)
	logger := t.Base.Logger.WithValues("event", operator.DeleteEvent, "nodestore", nodestore.Name)
	if !ok {
		logger.Info("Failed to cast object to nodestore in delete handler")
		return
	}

	logger.Info("Deleting nodestore")
	//o.enqueue(nodestore, DeleteEvent)

}
