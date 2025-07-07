package operator

import (
	"fmt"

	"k8s.io/client-go/tools/cache"
)

func (b *BaseOperator) enqueue(obj any, event EventType) {

	// check if every node has an existing nodestore

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		b.Logger.Error(err, "Error in getting key for object", obj)
		return
	}

	//wrap the key with the event type
	wrappedKey := fmt.Sprintf("%s:%s", event, key)

	b.Logger.Info("Adding key to workqueue", wrappedKey)
	b.Workqueue.Add(wrappedKey)

}
