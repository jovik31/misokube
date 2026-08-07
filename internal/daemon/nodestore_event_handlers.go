package daemon

import (
	seterav1 "github/setera/pkg/api/setera.com/v1"
	"k8s.io/client-go/tools/cache"
)

func (o *Operator) addNodestoreEventHandler(obj interface{}) {
	nodeStore, ok := obj.(*seterav1.NodeStore)
	if !ok {
		o.logger.WithValues("event", EventAdd).Info("failed to cast object to nodestore in add handler")
		return
	}

	if nodeStore.Spec.Name == o.nodeName {
		// local NodeStore: run the usual add reconcile (finalizers, etc.)
		o.logger.WithValues("event", EventAdd, "nodestore", nodeStore.Name).Info("enqueue nodestore add")
		o.base.EnqueueObjectWith(SourceNodeStoreCRD, EventAdd, nodeStore)
		return
	}

	// remote NodeStore: seed peer reconciliation immediately so EnsurePeer runs at least once.
	o.logger.WithValues("event", EventAdd, "nodestore", nodeStore.Name).Info("enqueue remote nodestore sync")
	o.base.EnqueueObjectWith(SourceNodeStoreCRD, EventUpdate, nodeStore)

}

// Enqueue NodeStore updates for both local and remote nodes. Local updates carry
// pod/ifindex changes needed by tc_podIDs even though they do not need peer setup.
func (o *Operator) updateNodestoreEventHandler(oldObj, newObj interface{}) {
	var oldNS, newNS *seterav1.NodeStore

	switch v := oldObj.(type) {
	case *seterav1.NodeStore:
		oldNS = v
	case cache.DeletedFinalStateUnknown:
		// Tombstones carry the last known object when a delete/update slipped past our cache.
		// We need to handle them to avoid panics when informers replay events after a relist.
		if ns, ok := v.Obj.(*seterav1.NodeStore); ok {
			oldNS = ns
		}
	}
	switch v := newObj.(type) {
	case *seterav1.NodeStore:
		newNS = v
	case cache.DeletedFinalStateUnknown:
		if ns, ok := v.Obj.(*seterav1.NodeStore); ok {
			newNS = ns
		}
	}
	if oldNS == nil || newNS == nil {
		o.logger.WithValues("event", EventUpdate).Info("skipping nodestore update: unable to decode objects")
		return
	}

	if equalTenantInfraForPeers(oldNS.Status.Tenants, newNS.Status.Tenants) {
		return
	}

	o.logger.WithValues("event", EventUpdate, "nodestore", newNS.Name).Info("enqueue remote nodestore update")
	o.base.EnqueueObjectWith(SourceNodeStoreCRD, EventUpdate, newNS)
}

// equalTenantInfraForPeers compares tenant maps while ignoring pod lists.
func equalTenantInfraForPeers(a, b map[string]seterav1.TenantInfra) bool {
	if len(a) != len(b) {
		return false
	}
	for tenant, oldInfo := range a {
		newInfo, ok := b[tenant]
		if !ok {
			return false
		}
		if oldInfo.TenantCIDR != newInfo.TenantCIDR ||
			oldInfo.VNI != newInfo.VNI ||
			oldInfo.VTEP_NAME != newInfo.VTEP_NAME ||
			oldInfo.VTEP_IP != newInfo.VTEP_IP ||
			oldInfo.VTEP_MAC != newInfo.VTEP_MAC ||
			oldInfo.BRIDGE_NAME != newInfo.BRIDGE_NAME ||
			oldInfo.BRIDGE_IP != newInfo.BRIDGE_IP ||
			oldInfo.BRIDGE_MAC != newInfo.BRIDGE_MAC ||
			!comparePodLists(oldInfo.Pods, newInfo.Pods) {
			return false
		}
	}
	return true
}

// Compare pod lists nowing they might not be ordered the same
func comparePodLists(oldList, newList []seterav1.Pod_Info) bool {
	if len(oldList) != len(newList) {
		return false
	}
	oldMap := make(map[string]seterav1.Pod_Info)
	for _, pod := range oldList {
		oldMap[pod.Name] = pod
	}
	newMap := make(map[string]seterav1.Pod_Info)
	for _, pod := range newList {
		newMap[pod.Name] = pod
	}
	for name, oldPod := range oldMap {
		newPod, ok := newMap[name]
		// If the pod is missing in the new list
		if !ok {
			return false
		}
		//If the IP or interface ID are not the same
		if oldPod.IP != newPod.IP || oldPod.Ifindex != newPod.Ifindex {
			return false
		}
	}
	return true
}

func (o *Operator) deleteEventNodestoretHandler(obj interface{}) {}
