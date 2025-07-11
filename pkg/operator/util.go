package operator

import (

	//std
	"fmt"
	"strings"

	//client-go
	"k8s.io/client-go/tools/cache"
)

func (b *BaseOperator) Enqueue(obj any, event EventType) {

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

func (b *BaseOperator) EnqueueWithKey(event EventType, key string) {

	// wrap the key with the event type
	wrappedKey := fmt.Sprintf("%s:%s", event, key)

	b.Logger.Info("Adding key to workqueue", wrappedKey)
	b.Workqueue.Add(wrappedKey)

}

func ParseQueuedKey(wrappedKey string) (EventType, string) {

	parts := strings.Split(wrappedKey, ":")
	if len(parts) < 2 {
		return UnknownEvent, wrappedKey
	}
	event := EventType(parts[0])
	key := parts[1]
	return event, key
}
