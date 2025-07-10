package orchestrator

import (
	// api types

	seterav1 "github/setera/pkg/api/setera.com/v1"
)

type Event string

const (
	AddEvent     Event = "Add"
	UpdateEvent  Event = "Update"
	DeleteEvent  Event = "Delete"
	UnknownEvent Event = "Unknown"
)

// add tenant key to the workqueue - add event
func (t *TenantOperator) addTenantHandler(obj any) {

	tenant, ok := obj.(*seterav1.Tenant)
	logger := t.Base.Logger.WithValues("event", AddEvent, "tenant", tenant.Name)

	if !ok {
		logger.Info("Failed to cast object to tenant in add handler")

	}
	logger.Info("Adding tenant to queue")

	//o.enqueue(tenant, AddEvent)

}

// add tenant key to the workqueue - update event
func (t *TenantOperator) updateTenantHandler(oldObj, newObj any) {

	oldTenant, ok := oldObj.(*seterav1.Tenant)
	logger := t.Base.Logger.WithValues("event", UpdateEvent, "tenant", oldTenant.Name)
	if !ok {
		logger.Info("Failed to cast old object to tenant in update handler")
		return
	}

	newTenant, ok := newObj.(*seterav1.Tenant)
	if !ok {
		logger.Info("Failed to cast new object to tenant in update handler")
		return
	}

	if oldTenant.ResourceVersion == newTenant.ResourceVersion {
		logger.Info("Resource version has not changed, skipping update event for tenant")
		return
	}

	// Only add update event for tenants where the number of nodes has changed
	if len(oldTenant.Spec.Nodes) != len(newTenant.Spec.Nodes) {

		logger.Info("Number of nodes changed in tenant")
		logger.Info("Add tenant to queue - Update event")
		//o.enqueue(newTenant, UpdateEvent)
		return

	}

}

// add tenant key to the workqueue - deletion event
func (t *TenantOperator) deleteTenantHandler(obj any) {

	tenant, ok := obj.(*seterav1.Tenant)
	logger := t.Base.Logger.WithValues("event", UpdateEvent, "tenant", tenant.Name)
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
	logger := t.Base.Logger.WithValues("event", UpdateEvent, "nodestore", oldNodestore.Name)
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
		logger.Info("Resource version has not changed, skipping update event for nodestore")
		return
	}

	logger.Info("Updating nodestore")
	//o.enqueue(newNodestore, UpdateEvent)

}

// add tenants key in deleted nodestore for processing
func (t *TenantOperator) deleteTenantFromNodestoreHandler(obj any) {

	nodestore, ok := obj.(*seterav1.NodeStore)
	logger := t.Base.Logger.WithValues("event", DeleteEvent, "nodestore", nodestore.Name)
	if !ok {
		logger.Info("Failed to cast object to nodestore in delete handler")
		return
	}

	logger.Info("Deleting nodestore")
	//o.enqueue(nodestore, DeleteEvent)

}
