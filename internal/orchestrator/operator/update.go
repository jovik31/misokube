package orchestrator

import (
	"context"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/operator"

	"k8s.io/client-go/tools/cache"

	// client-go
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (o *TenantOperator) updateTenant(key string) error {

	// add tenant to the orchestrator
	return nil
}

func (t *TenantOperator) updateFromNodestore(key string) error {

	t.Base.Logger.Info("Updating tenant from NodeStore", "key", key)

	// get the nodestore key
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		t.Base.Logger.Error(err, "Failed to split key", "key", key)
		return err
	}

	// since this is an update fetch nodestore from API
	nodestore, err := t.Base.Seterav1Clientset.SeteraV1().NodeStores(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Base.Logger.Error(err, "Failed to get NodeStore", "nodestore", name)
		return err
	}
	// check if the nodestore is nil
	if nodestore == nil {
		t.Base.Logger.Info("NodeStore is nil, skipping update", "nodestore", name)
		return nil
	}

	// retrive the tenants from the nodestore
	mod := nodestore.DeepCopy()

	tenants := mod.Status.Tenants

	// no tenants configed in the nodestore
	if len(tenants) == 0 {
		t.Base.Logger.Info("No tenants found in NodeStore", "nodestore", name)

		return nil
	}

	// get all tenants in the cluster
	/*tenantsList, err := t.Base.Seterav1Clientset.SeteraV1().Tenants(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Base.Logger.Error(err, "Failed to list Tenants", "nodestore", name)
		return err
	}

	for _, tenant := range tenantsList.Items {
		t.Base.Logger.Info("Processing Tenant", "tenant", tenant.Name)
	}*/

	// iterate over the tenants, get the tenant object and update it
	for tenantName, tenantInfra := range tenants {

		// get the tenant object
		tenant, err := t.Base.Seterav1Clientset.SeteraV1().Tenants(namespace).Get(context.TODO(), tenantName, metav1.GetOptions{})
		if err != nil {
			t.Base.Logger.Error(err, "Failed to get Tenant", "tenant", tenantName)
			continue
		}

		tmod := tenant.DeepCopy()

		// place the node information in the tenant
		nodestoreInfo := seterav1.NodeInfo{
			Name:       nodestore.Name,
			NodeIP:     mod.Spec.NodeIP,
			TenantCIDR: tenantInfra.TenantCIDR,
			VtepIP:     tenantInfra.VTEP_IP,
			VtepMAC:    tenantInfra.VTEP_MAC,
		}

		// check if the node is already assigned to the tenant
		if t.checkAssignedNodes(tmod.Status.AssignedNodes, nodestore.Name) {
			t.Base.Logger.Info("Node already assigned to Tenant", "nodestore", nodestore.Name, "tenant", tenantName)
			continue // skip this tenant if the node is already assigned
		}

		tmod.Status.AssignedNodes = append(tmod.Status.AssignedNodes, nodestoreInfo)

		// remove node from awaiting nodes slice
		awaitingNodes := tmod.Status.AwaitingNodeConfiguration

		// remove the node from the awaiting nodes slice
		awaitingNodes = operator.RemoveIndex(awaitingNodes, mod.Name)

		// update the awaiting nodes slice
		tmod.Status.AwaitingNodeConfiguration = awaitingNodes

		// update the tenant object in the k8s cluster
		err = t.updateTenantObject(tmod)
		if err != nil {
			t.Base.Logger.Error(err, "Failed to update Tenant", "tenant", tenantName)
			continue // skip this tenant if it cannot be updated
		}

	}

	return nil
}

func (t *TenantOperator) checkAssignedNodes(nodeAssigned []seterav1.NodeInfo, nodestoreName string) bool {

	// checj if node assigned is nil
	if nodeAssigned == nil {
		return false // node is not assigned
	}

	for _, node := range nodeAssigned {
		if node.Name == nodestoreName {
			t.Base.Logger.Info("Node already assigned to Tenant", "nodestore", nodestoreName)
			return true // node is already assigned
		}
	}

	return false
}
