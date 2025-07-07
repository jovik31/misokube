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
func (o *OrchOperator) addTenantHandler(obj any) {

	tenant, ok := obj.(*seterav1.Tenant)
	logger := o.Base.Logger.WithValues("event", AddEvent, "tenant", tenant.Name)

	if !ok {
		logger.Info("Failed to cast object to tenant in add handler")

	}
	logger.Info("Adding tenant to queue")

	//o.enqueue(tenant, AddEvent)

}

// add tenant key to the workqueue - update event
func (o *OrchOperator) updateTenantHandler(oldObj, newObj any) {

	oldTenant, ok := oldObj.(*seterav1.Tenant)
	logger := o.Base.Logger.WithValues("event", UpdateEvent, "tenant", oldTenant.Name)
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
func (o *OrchOperator) deleteTenantHandler(obj any) {

	tenant, ok := obj.(*seterav1.Tenant)
	logger := o.Base.Logger.WithValues("event", UpdateEvent, "tenant", tenant.Name)
	if !ok {
		logger.Info("Failed to cast object to tenant in delete handler")
		return
	}

	logger.Info("Deleting tenant")
	//o.enqueue(tenant, DeleteEvent)

}
