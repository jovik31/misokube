package nodewatcher

import "k8s.io/client-go/tools/cache"

func (w *Watcher) registerEventHandlers() {
	w.nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(any) {
			w.queue.Add(fullSyncKey)
		},
		UpdateFunc: func(_, _ any) {
			w.queue.Add(fullSyncKey)
		},
		DeleteFunc: func(any) {
			w.queue.Add(fullSyncKey)
		},
	})
}