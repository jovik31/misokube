package doperator

/*import (

	// internals
	"context"
	"fmt"

	// api types

	// configs
	config "github/setera/pkg"

	// k8s

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// client-go
	"k8s.io/client-go/tools/cache"
)*/

// config nodestore that is called from a tenant that added them to the awaiting nodes

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

// Configure the tenant in the nodestor

func (n *NodeStoreOperator) configTenant(key string) error {

	n.Base.Logger.Info("Configuring Tenant", "key", key)

	// get the tenant key
	/*namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		n.Base.Logger.Error(err, "Failed to split key", "key", key)
		return err
	}

	// fetch tenant from cache
	/*tenant, err := n.TenantLister.Tenants(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			n.Base.Logger.Error(err, "Tenant not found in cache", "key", key)
			return fmt.Errorf("tenant %s not found in namespace %s", name, namespace)
		} else {
			n.Base.Logger.Error(err, "Error fetching Tenant from cache", "key", key)
			return err
		}
	}

	// fetch local nodestore from api
	/*nodestore, err := n.Base.Seterav1Clientset.SeteraV1().NodeStores(config.SeteraNamespace).Get(context.Background(), n.nodeName, metav1.GetOptions{})
	if err != nil {
		n.Base.Logger.Error(err, "Failed to get local NodeStore from API", "nodestore", n.nodeName)
		return err
	}*/

	// local copy
	//mod := nodestore.DeepCopy()

	// check if the tenant is already configured in the nodestore
	//remoteInfra, existsNodestore := nodestore.Status.Tenants[tenant.Name]

	/*infra, existsNode, err := n.NetService.GetTenantRecord(tenant.Name)
	if err != nil {
		n.Base.Logger.Error(err, "Failed to get tenant infrastructure", "tenant", tenant.Name)
		return fmt.Errorf("failed to get tenant %s infrastructure: %v", tenant.Name, err)
	}

	if existsNodestore && existsNode {

		// check if the tenant infra is the same as the one in the nodestore
		if !equality.Semantic.DeepEqual(infra, &remoteInfra) {

			mod.Status.Tenants[tenant.Name] = *infra

			// if the tenant infra is not the same, update the nodestore with the new tenant infra
			_, err := n.Base.Seterav1Clientset.SeteraV1().NodeStores(config.SeteraNamespace).Update(context.Background(), mod, metav1.UpdateOptions{})
			if err != nil {
				n.Base.Logger.Error(err, "Failed to update NodeStore status", "nodestore", mod.Name)
				return fmt.Errorf("failed to update nodestore %s status: %v", mod.Name, err)
			}
			n.Base.Logger.Info("Updated NodeStore with new tenant infrastructure", "tenant", tenant.Name, "nodestore", mod.Name)
			return nil
		}

		n.Base.Logger.Info("[INFO] - Tenant exists in NodeStore", "tenant", tenant.Name, "nodestore", nodestore.Name)
		return nil
	}

	// allocate tenant
	configed_tenant_infra, err := n.NetService.AllocateTenant(tenant.Name)
	if err != nil {

		return fmt.Errorf("failed to allocate tenant %s infrastructure: %v", tenant.Name, err)
	}
	configed_tenant_infra.Pods = make([]seterav1.Pod_Info, 0)
	// patch the nodestore with the tenant infrastructure
	if nodestore.Status.Tenants == nil {
		nodestore.Status.Tenants = make(map[string]seterav1.TenantInfra)
	}

	// add the tenant infra to the nodestore status
	mod.Status.Tenants[tenant.Name] = *configed_tenant_infra
	// update the nodestore with the tenant infra
	_, err = n.Base.Seterav1Clientset.SeteraV1().NodeStores(config.SeteraNamespace).Update(context.Background(), mod, metav1.UpdateOptions{})
	if err != nil {
		n.Base.Logger.Error(err, "Failed to update NodeStore status", "nodestore", mod.Name)
		return fmt.Errorf("failed to update nodestore %s status: %v", mod, err)
	}*/

	return nil
}
