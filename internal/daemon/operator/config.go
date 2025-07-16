package daemon

import (

	// internals
	"fmt"

	// api types
	seterav1 "github/setera/pkg/api/setera.com/v1"

	// configs
	configs "github/setera/internal"

	// internals
	"github/setera/pkg/operator"
	// k8s
	"k8s.io/apimachinery/pkg/api/errors"

	// client-go
	"k8s.io/client-go/tools/cache"
)

// Config holds the configuration for the NodeStoreOperator - called when a new tenant is created
func (n *NodeStoreOperator) configNodestore(key string) error {

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

	// create a copy of the nodestore
	mod := nodestore.DeepCopy()

	// ensure finalizer is present
	if !operator.ContainsString(nodestore.Finalizers, configs.NodeStoreFinalizer) {
		mod.Finalizers = append(mod.Finalizers, configs.NodeStoreFinalizer)
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

		// check if the tenant is already configured in this node or is configured in the tenant
		// Call network manager to perform tenant configuration and return a TenantInfo object

	}

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
