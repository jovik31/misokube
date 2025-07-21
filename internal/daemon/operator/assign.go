package daemon

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"

	seterav1 "github/setera/pkg/api/setera.com/v1"
)

// called when the node is part of the assigned slice in the tenant
func (n *NodeStoreOperator) assignedNodestore(key string) error {

	ctx := context.Background()

	// get the tenant
	// extract the assigned nodes
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("failed to split key %s: %w", key, err)
	}

	// fetch tenant from cache
	tenant, err := n.TenantLister.Tenants(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {

			return fmt.Errorf("tenant %s not found in namespace %s", name, namespace)

		} else {
			return fmt.Errorf("failed to get tenant %s in namespace %s: %w", name, namespace, err)
		}
	}

	// copy the tenant
	mod := tenant.DeepCopy()

	// apply routing between the nodes of the same tenant
	// 1. retrieve all nodes from the tenant
	// 2. For each node in the tenant setup routes for the other nodes in the tenant

	assignedNodes := mod.Status.AssignedNodes
	if len(assignedNodes) == 0 {
		return fmt.Errorf("tenant %s in namespace %s has no assigned nodes", name, namespace)
	}

	// get this node info
	localNodeInfo, err := n.getLocalNodeInfo(assignedNodes)
	if err != nil {
		return fmt.Errorf("failed to get local node info for node %s in tenant %s: %w", n.nodeName, name, err)
	}
	for _, node := range assignedNodes {

		// only setup routes for the remote nodes, not the self
		if node.Name == n.nodeName {
			continue // skip self
		}

		err := n.NetService.ConfigureTenantRoutes(ctx, localNodeInfo, node)
		if err != nil {
			return fmt.Errorf("failed to configure tenant routes for node %s in tenant %s: %w", node.Name, name, err)
		}
	}

	return nil
}

func (n *NodeStoreOperator) getLocalNodeInfo(nodeInfoList []seterav1.NodeInfo) (seterav1.NodeInfo, error) {

	for _, nodeInfo := range nodeInfoList {
		if nodeInfo.Name == n.nodeName {
			return nodeInfo, nil
		}
	}
	return seterav1.NodeInfo{}, fmt.Errorf("node %s not found in nodeInfoList", n.nodeName)
}
