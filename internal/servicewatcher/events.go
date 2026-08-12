package servicewatcher

import (
	"reflect"

	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

func (w *Watcher) registerEventHandlers() {
	w.serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.enqueueFullSync,
		UpdateFunc: func(_, _ any) { w.enqueueFullSync(nil) },
		DeleteFunc: w.enqueueFullSync,
	})

	w.endpointSliceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.enqueueFullSync,
		UpdateFunc: func(_, _ any) { w.enqueueFullSync(nil) },
		DeleteFunc: w.enqueueFullSync,
	})

	w.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.enqueueFullSync,
		UpdateFunc: w.onPodUpdate,
		DeleteFunc: w.enqueueFullSync,
	})
}

func (w *Watcher) enqueueFullSync(_ any) {
	w.queue.Add(fullSyncKey)
}

func (w *Watcher) onPodUpdate(oldObj, newObj any) {
	oldPod, oldOK := oldObj.(*corev1.Pod)
	newPod, newOK := newObj.(*corev1.Pod)
	if !oldOK || !newOK || oldPod == nil || newPod == nil {
		return
	}

	oldTenant := tenantmeta.ResolvePodTenant(
		oldPod.Namespace,
		oldPod.Labels,
	)
	newTenant := tenantmeta.ResolvePodTenant(
		newPod.Namespace,
		newPod.Labels,
	)

	if oldTenant != newTenant ||
		oldPod.Spec.HostNetwork != newPod.Spec.HostNetwork ||
		oldPod.Status.PodIP != newPod.Status.PodIP ||
		!reflect.DeepEqual(oldPod.Status.PodIPs, newPod.Status.PodIPs) {
		w.queue.Add(fullSyncKey)
	}
}
