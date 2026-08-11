package podwatcher

import (
	"reflect"

	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

func (w *Watcher) registerEventHandlers() {
	w.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onPodAdd,
		UpdateFunc: w.onPodUpdate,
		DeleteFunc: w.onPodDelete,
	})

	w.nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onNodeAdd,
		UpdateFunc: w.onNodeUpdate,
		DeleteFunc: w.onNodeDelete,
	})
}

func (w *Watcher) onPodAdd(obj any) {
	w.enqueuePod(obj)
}

func (w *Watcher) onPodUpdate(_, newObj any) {
	w.enqueuePod(newObj)
}

func (w *Watcher) onPodDelete(obj any) {
	w.enqueuePod(obj)
}

func (w *Watcher) onNodeAdd(_ any) {
	// New Node state may make Pods on that Node relevant.
	w.queue.Add(fullSyncKey)
}

func (w *Watcher) onNodeUpdate(oldObj, newObj any) {
	oldNode, oldOK := nodeFromObject(oldObj)
	newNode, newOK := nodeFromObject(newObj)
	if !oldOK || !newOK {
		return
	}

	// Remote relevance depends only on Setera tenant membership. This applies
	// to the local Node and to remote Nodes hosting explicit-tenant Pods.
	if !reflect.DeepEqual(
		tenantmeta.Tenants(oldNode.Labels),
		tenantmeta.Tenants(newNode.Labels),
	) {
		w.queue.Add(fullSyncKey)
	}
}

func (w *Watcher) onNodeDelete(_ any) {
	// Pods on the removed Node must be reconsidered, and deletion of the local
	// Node should prevent us from keeping stale remote identities.
	w.queue.Add(fullSyncKey)
}

func (w *Watcher) enqueuePod(obj any) {
	pod, ok := podFromObject(obj)
	if !ok {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(pod)
	if err != nil || key == "" {
		return
	}

	w.queue.Add(key)
}

func podFromObject(obj any) (*corev1.Pod, bool) {
	switch value := obj.(type) {
	case *corev1.Pod:
		return value, value != nil

	case cache.DeletedFinalStateUnknown:
		pod, ok := value.Obj.(*corev1.Pod)
		return pod, ok && pod != nil

	default:
		return nil, false
	}
}

func nodeFromObject(obj any) (*corev1.Node, bool) {
	switch value := obj.(type) {
	case *corev1.Node:
		return value, value != nil

	case cache.DeletedFinalStateUnknown:
		node, ok := value.Obj.(*corev1.Node)
		return node, ok && node != nil

	default:
		return nil, false
	}
}
