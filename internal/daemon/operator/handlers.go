package daemon

import (

	//std

	// internal pkg
	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/operator"
)

const (
	WaitingNodeTenantEvent  operator.EventType = "Config"
	AssignedNodeTenantEvent operator.EventType = "Assigned"
)

func (n *NodeStoreOperator) addNodeStoreHandler(obj any) {
	// Add logic to handle adding a NodeStore
}
func (n *NodeStoreOperator) updateNodeStoreHandler(oldObj, newObj any) {
	// Add logic to handle updating a NodeStore
}
func (n *NodeStoreOperator) deleteNodeStoreHandler(obj any) {
	// Add logic to handle deleting a NodeStore
}

func (n *NodeStoreOperator) updateNodestoreFromTenantHandler(oldObj, newObj any) {
	// Add logic to handle updating NodeStore from Tenant
	newTenant, ok := newObj.(*seterav1.Tenant)

	logger := n.Base.Logger.WithValues("event", operator.UpdateEvent, "tenant", newTenant.Name)
	if !ok {
		logger.Info("failed to cast old object to tenant in update handler")
		return
	}

	// extract nodes from newTenant
	nodes := newTenant.Status.AwaitingNodeConfiguration
	for _, node := range nodes {

		// node belongs to tenant and requires config
		if node == n.Base.Name {
			logger.Info("tenant has node awaiting configuration", "node", node)

			nodeStore, err := n.NodeStoreLister.NodeStores("").Get(n.Base.Name)
			if err != nil {
				logger.Error(err, "failed to get NodeStore for node", "node", n.Base.Name)
				return
			}

			// trigger tenant configuration for this node
			n.Base.Enqueue(nodeStore, WaitingNodeTenantEvent)
		} else {
			logger.Info("tenant does not have node awaiting configuration", "node", node)
		}
	}

	configedNodes := newTenant.Status.AssignedNodes
	for _, node := range configedNodes {
		// node belongs to tenant and is configured
		if node.Name == n.Base.Name {
			logger.Info("tenant has node configured", "node", node)

			// trigger updateNodeStore for this node
			n.Base.Enqueue(node, AssignedNodeTenantEvent)
		} else {
			logger.Info("tenant does not have node configured", "node", node)
		}
	}
}

func (n *NodeStoreOperator) deleteNodestoreFromTenantHandler(obj any) {
	// Add logic to handle deleting NodeStore from Tenant
}
func (n *NodeStoreOperator) addPod(obj any) {
	// Add logic to handle adding a Pod
	// This could involve checking if the Pod is associated with a NodeStore
	// and then triggering any necessary updates or actions.
}
