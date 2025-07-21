package daemon

import (

	// internals
	"context"
	"fmt"

	// api types
	seterav1 "github/setera/pkg/api/setera.com/v1"

	// configs
	config "github/setera/pkg"

	// internals
	"github/setera/pkg/operator"
	// k8s
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// client-go
	"k8s.io/client-go/tools/cache"
)

// config nodestore that is called from a tenant that added them to the awaiting nodes

// Config holds the configuration for the NodeStoreOperator - called when a new tenant is created
func (n *NodeStoreOperator) configNodestore(key string) error {

	ctx := context.Background()

	n.Base.Logger.Info("Configuring NodeStore", "key", key)

	// get the nodestore key
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		n.Base.Logger.Error(err, "Failed to split key", "key", key)
		return err
	}

	// fetch nodestore from cache
	nodestore, err := n.NodeStoreLister.NodeStores(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			n.Base.Logger.Error(err, "NodeStore not found in cache", "key", key)
			return fmt.Errorf("nodestore %s not found in namespace %s", name, namespace)
		} else {
			n.Base.Logger.Error(err, "Error fetching NodeStore from cache", "key", key)
			return err
		}
	}

	n.Base.Logger.Info("DEBUG INFO", "nodestore", nodestore.Name, "namespace", nodestore.Namespace)

	// create a copy of the nodestore
	mod := nodestore.DeepCopy()

	// ensure finalizer is present
	if !operator.ContainsString(nodestore.Finalizers, config.NodeStoreFinalizer) {
		mod.Finalizers = append(mod.Finalizers, config.NodeStoreFinalizer)
		n.Base.Logger.Info("Adding finalizer to NodeStore", "nodestore", nodestore.Name)
	}

	// check the tenant where this node is in the awaiting node array
	waitingTenants, err := n.TenantInformer.GetIndexer().ByIndex("awaitingNodes", nodestore.Name)
	if err != nil {
		n.Base.Logger.Error(err, "Failed to get tenants awaiting node configuration", "node", n.Base.Name)
	}

	for _, tenantObj := range waitingTenants {
		tenant, ok := tenantObj.(*seterav1.Tenant)
		if !ok {
			n.Base.Logger.Error(nil, "Failed to cast tenant object", "tenantObj", tenantObj)
		}
		n.Base.Logger.Info("Configuring NodeStore for tenant", "tenant", tenant.Name)

		configed_tenant_infra, err := n.NetService.AllocateTenant(tenant.Name)
		if err != nil {
			n.Base.Logger.Error(err, "Failed to allocate tenant infrastructure", "tenant", tenant.Name)
		}
		configed_tenant_infra.Pods = make([]seterav1.Pod_Info, 0)
		// patch the nodestore with the tenant infrastructure
		mod.Status.Tenants[tenant.Name] = *configed_tenant_infra

		// get the nodestore
		nodestore, err := n.NodeStoreLister.NodeStores("default").Get(nodestore.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				n.Base.Logger.Error(err, "NodeStore not found", "nodestore", nodestore.Name)
			}
		}
		n.Base.Logger.Info("DEBUG NODESTORE FOUND", "nodestore", nodestore.Name)

		// patch the nodestore with the tenant infra
		_, err = n.Base.Seterav1Clientset.SeteraV1().NodeStores("default").UpdateStatus(ctx, mod, metav1.UpdateOptions{})
		if err != nil {
			n.Base.Logger.Error(err, "Failed to update NodeStore status", "nodestore", nodestore.Name)
			return fmt.Errorf("failed to update nodestore %s status: %v", nodestore.Name, err)
		}
		n.Base.Logger.Info("NodeStore status updated", "nodestore", nodestore.Name, "tenant", tenant.Name)

		// remove the tenant from the awaiting nodes

	}

	// update the nodestore with the network configuration

	// for all the tenants check if it is already configured in this node (nodestore.Status.Tenants)
	// if so get the tenant infra and check the vailidity of the configuration and return.

	// for each tenant where it is an awaiting node

	//network config

	// allocate tenant network[IP CIDR]
	// vni 1 identifies the default tenant
	// calculate VNI - hash with limits from 2-16777214 - same tenant name same int
	// allocate tenant vtep[IP, MAC]
	// allocate tenant bridge[IP, MAC]
	// add local routes

	// add network info to nodestore

	// route config - inter node communication
	// check if tenant has configedNodes

	// for each config node in the same tenant
	// check if routes already exist
	// fdb
	// arp
	// routes

	// On this node store check if there is more tenants
	// If the tenant is not default
	// Add iptable rules to it

	// Observe the default tenant

	// Create SNAT rules for the tenant comunication
	// Create Forward rules for the node CIDR

	// Configure the tenant in the nodestore
	return nil
}
