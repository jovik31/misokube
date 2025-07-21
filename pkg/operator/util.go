package operator

import (

	//std
	"fmt"
	"strings"

	//client-go
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/tools/cache"

	// setera api types
	seterav1 "github/setera/pkg/api/setera.com/v1"
)

func (b *BaseOperator) Enqueue(obj any, event EventType) {

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		b.Logger.Error(err, "Error in getting key for object", obj)
		return
	}

	//wrap the key with the event type
	wrappedKey := fmt.Sprintf("%s:%s", event, key)

	b.Logger.WithValues("event", event, "key", key).Info("ENQUEUED")
	b.Workqueue.Add(wrappedKey)

}

func (b *BaseOperator) EnqueueWithKey(event EventType, key string) {

	// wrap the key with the event type
	wrappedKey := fmt.Sprintf("%s:%s", event, key)

	b.Logger.WithValues("event", event, "key", key).Info("ENQUEUED WITH KEY")
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

func ContainsString(slice []string, item string) bool {

	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

//func jsonpaytload

func IsFinalizerRvUpdate_tenant(old, new *seterav1.Tenant) bool {
	// Check if only finalizers are updated
	oldCopy := old.DeepCopy()
	newCopy := new.DeepCopy()

	// remove the finalizers for comparison
	oldCopy.Finalizers = nil
	newCopy.Finalizers = nil

	oldCopy.ObjectMeta.ResourceVersion = ""
	newCopy.ObjectMeta.ResourceVersion = ""

	return equality.Semantic.DeepEqual(oldCopy, newCopy)

}

func IsFinalizerRvUpdate_nodestore(old, new *seterav1.NodeStore) bool {

	// Check if only finalizers are updated
	oldCopy := old.DeepCopy()
	newCopy := new.DeepCopy()

	// remove the finalizers for comparison
	oldCopy.Finalizers = nil
	newCopy.Finalizers = nil

	oldCopy.ObjectMeta.ResourceVersion = ""
	newCopy.ObjectMeta.ResourceVersion = ""

	return equality.Semantic.DeepEqual(oldCopy, newCopy)
}

func RemoveIndex[T comparable](slice []T, val T) []T {
	idx := -1
	for i, v := range slice {
		if v == val {
			idx = i
			break
		}
	}
	if idx < 0 {
		// not found; nothing to remove
		return slice
	}
	// remove at idx
	return append(slice[:idx], slice[idx+1:]...)
}
