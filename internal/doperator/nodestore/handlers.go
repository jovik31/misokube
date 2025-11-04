package nodestore

import (

	//std

	// internal pkg

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/operator"
	"slices"

	// k8s
	"k8s.io/apimachinery/pkg/api/equality"
)

const (
	WaitingNodeTenantEvent  operator.EventType = "Config"
	AssignedNodeTenantEvent operator.EventType = "Assigned"
	DeletedTenantEvent      operator.EventType = "Remove"
)

// from nodestoreInformer --> add event
func (n *NodeStoreOperator) addNodeStoreHandler(obj any) {

	logger := n.Base.Logger.WithValues("event", operator.AddEvent)
	nodestore, ok := obj.(*seterav1.NodeStore)
	if !ok {
		logger.Info("Failed to cast object to NodeStore in add handler")

	}

	// check if this is my nodestore
	if nodestore.Name != n.nodeName {
		logger.WithValues("nodestore", nodestore.Name, "node", n.nodeName).Info("NodeStore is not for this node, skipping")
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

	// what are the update scenarios we need to handle
	// 1. adding tenant information - configure the node
	// 2. change in tenant information - reconfigure the nodestore

}

// from nodestoreInformer --> delete event
func (n *NodeStoreOperator) deleteNodeStoreHandler(obj any) {
	// Add logic to handle deleting a NodeStore
}

// from tenantInformer --> config event
func (n *NodeStoreOperator) updateFromTenantHandler(oldObj, newObj any) {

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

	// check if the node is in the tenant's awaiting node configuration or assigned nodes
	if !slices.Contains(newTenant.Status.AwaitingNodeConfiguration, n.nodeName) && !n.checkAssignedNodes(newTenant) {
		return
	}

	// the node needs to be configured
	if slices.Contains(newTenant.Status.AwaitingNodeConfiguration, n.nodeName) {
		n.Base.Enqueue(newTenant, WaitingNodeTenantEvent)
	}

	// if the node is already assigned, enqueue it for processing
	if n.checkAssignedNodes(newTenant) {
		n.Base.Enqueue(newTenant, AssignedNodeTenantEvent)

	}
}

// from tenantInformer --> delete event
func (n *NodeStoreOperator) deleteFromTenantHandler(obj any) {

	newTenant, ok := obj.(*seterav1.Tenant)
	if !ok {
		n.Base.Logger.Error(nil, "failed to cast new object to tenant in delete handler")
	}

	// check if the local nodestore is in the tenant
	if n.checkAssignedNodes(newTenant) {
		n.Base.Enqueue(newTenant, DeletedTenantEvent)
	}

}

// from podInformer --> add event
func (n *NodeStoreOperator) addPod(obj any) {
	// Add logic to handle adding a Pod
	// This could involve checking if the Pod is associated with a NodeStore
	// and then triggering any necessary updates or actions.
}

func (n *NodeStoreOperator) checkAssignedNodes(tenant *seterav1.Tenant) bool {

	for _, node := range tenant.Status.AssignedNodes {
		if node.Name == n.nodeName {
			return true
		}
	}
	return false
}
