package orchestrator

import (

	// setera api tyes
	seterav1 "github/setera/pkg/api/setera.com/v1"

	// k8s

	// client-go
	"k8s.io/client-go/tools/cache"
)

const TenantFinalizer = "finalizer.setera.com"

func (o *TenantOperator) addTenant(key string) error {
	// add tenant to the k8s cluster
	/*[x get the tenant object from the cache
	[x retrieve the nodestores existing in the cluster //this part may not be needed if it is only applied
	[x] check if all nodestores are present
	[x] check if the tenant has a finalizer
	[ ] check the tenant selecctors and requirements
	[ ] place the tenants in the nodestores chosen for tenant deployment
	[ ] update the tenant status with the progress and the node list with the nodestores chosen
	[ ] update the tenant object in the k8s cluster
	[ ] update the nodestore object in the k8s cluster
	*/

	// check if all nodestores are present
	/*ok, reason := o.checkNodeNodeStore()
	if !ok {
		err := errors.New(reason)
		o.logger.Error(err, "Failed to add tenant", key)

		return err
	}*/

	// split the name and namespace from the key
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		o.Base.Logger.Error(err, "Error in getting key for object", key)
		return err
	}

	//get tenant object from the tenant lister (cache)
	tenant, err := o.TenantLister.Tenants(namespace).Get(name)
	if err != nil {
		o.Base.Logger.Error(err, "Error in getting tenant object from cache", key)
		return err
	}

	// create a copy of the tenant object for modification
	var updatedTenant *seterav1.Tenant
	updatedTenant = tenant.DeepCopy()

	// Check for tenant finalizer
	ok := o.checkTenantFinalizer(tenant)
	if !ok { // finalizer not found
		// add the finalizer to the tenant object

		updatedFinalizers := append(updatedTenant.Finalizers, TenantFinalizer)
		updatedTenant.Finalizers = updatedFinalizers

	}

	// FIXME: check if the tenant is already deployed
	//FIXME: The number of zones can be validated in a webhook, upon creation of the tenant object, as well as the existing selectors
	// extract the tenant zones and selectors
	_ = tenant.Spec.Zones // zones are equivalent to the number of nodes the tenant is going to be deployed to
	//selectors := tenant.Spec.Zones[].Requirements // the required selectors that the nodes must have for this tenant to be deployed to them

	// TODO: check the cache from the nodestore scores sent to the orchestrator from the local agents (daemons)

	// update the tenant object in the k8s cluster
	err = o.updateTenantObject(updatedTenant)
	if err != nil {
		o.Base.Logger.Error(err, "Failed to update tenant object in the k8s cluster", key)
		return err
	}

	return nil
}
