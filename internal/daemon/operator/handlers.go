package daemon

import (

	//std

	// internal pkg
	config "github/setera/pkg"
	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/operator"

	// k8s
	"k8s.io/apimachinery/pkg/api/equality"
)

const (
	WaitingNodeTenantEvent  operator.EventType = "Config"
	AssignedNodeTenantEvent operator.EventType = "Assigned"
)

// from nodestoreInformer --> add event
func (n *NodeStoreOperator) addNodeStoreHandler(obj any) {

	logger := n.Base.Logger.WithValues("event", operator.AddEvent)
	nodestore, ok := obj.(*seterav1.NodeStore)
	if !ok {
		logger.Info("Failed to cast object to NodeStore in add handler")

	} else {
		n.Base.Enqueue(nodestore, operator.AddEvent)

	}
}

// from nodestoreInformer --> update event
func (n *NodeStoreOperator) updateNodeStoreHandler(oldObj, newObj any) {

	// only updates done on the nodestore status shouyld be processed and allowed
	oldNodeStore := oldObj.(*seterav1.NodeStore)
	newNodestore := newObj.(*seterav1.NodeStore)

	if !equality.Semantic.DeepEqual(oldNodeStore.Status, newNodestore.Status) {

		logger := n.Base.Logger.WithValues("event", operator.UpdateEvent, "nodestore", newNodestore.Name)
		logger.Info("NodeStore status updated, adding to workqueue")

		n.Base.Enqueue(newNodestore, operator.UpdateEvent)
	}

}

// from nodestoreInformer --> delete event
func (n *NodeStoreOperator) deleteNodeStoreHandler(obj any) {
	// Add logic to handle deleting a NodeStore
}

// from tenantInformer --> config event
func (n *NodeStoreOperator) updateNodestoreFromTenantHandler(oldObj, newObj any) {
	// Add logic to handle updating NodeStore from Tenant
	newTenant, ok := newObj.(*seterav1.Tenant)
	if !ok {
		n.Base.Logger.Error(nil, "failed to cast new object to tenant in update handler")
		return
	}

	logger := n.Base.Logger.WithValues("event", operator.UpdateEvent, "tenant", newTenant.Name)
	if !ok {
		logger.Info("failed to cast old object to tenant in update handler")
		return
	}

	// extract nodes from newTenant
	nodes := newTenant.Status.AwaitingNodeConfiguration
	for _, node := range nodes {

		// node belongs to tenant and requires config
		if node == n.nodeName {
			logger.Info("tenant has node awaiting configuration", "node", node)

			nodeStore, err := n.NodeStoreLister.NodeStores(config.SeteraNamespace).Get(n.nodeName)
			if err != nil {
				logger.Error(err, "failed to get NodeStore for node", "node", n.nodeName)
				return
			}

			// before enqueuing, check if the tenant is already assigned to this node

			// trigger addNodeStore for this node
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

// from tenantInformer --> delete event
func (n *NodeStoreOperator) deleteNodestoreFromTenantHandler(obj any) {
	// Add logic to handle deleting NodeStore from Tenant
}

// from podInformer --> add event
func (n *NodeStoreOperator) addPod(obj any) {
	// Add logic to handle adding a Pod
	// This could involve checking if the Pod is associated with a NodeStore
	// and then triggering any necessary updates or actions.
}
