package daemon

import seterav1 "github/setera/pkg/api/setera.com/v1"

func (o *Operator) addNodestoreEventHandler(obj interface{}) {
	nodeStore, ok := obj.(*seterav1.NodeStore)
	if !ok {
		o.logger.WithValues("event", EventAdd).Info("failed to cast object to nodestore in add handler")
		return
	}

	// enqueue only if the nodestore belongs to this daemon's node
	if nodeStore.Spec.Name == o.nodeName {
		o.logger.WithValues("event", EventAdd, "nodestore", nodeStore.Name).Info("enqueue nodestore add")
		o.base.EnqueueObjectWith(SourceNodeStoreCRD, EventAdd, nodeStore)
	} else {
		o.logger.WithValues("event", EventAdd, "nodestore", nodeStore.Name).Info("skipping nodestore add for different node")
	}

}

// enqueue nodestore update events only for the remote nodestore's
func (o *Operator) updateNodestoreEventHandler(oldObj, newObj interface{}) {}

func (o *Operator) deleteEventNodestoretHandler(obj interface{}) {}
