package operator

import "k8s.io/client-go/tools/cache"

type Informer2EventFuncs struct {
	Informer   cache.SharedIndexInformer
	EventFuncs cache.ResourceEventHandlerFuncs
}

func (c *BaseOperator) RegisterEventHandlers(regs ...Informer2EventFuncs) {

	for _, r := range regs {

		if r.Informer == nil {
			c.logger.Error(nil, "informer is nil, cannot register event handlers")
			continue
		}
		c.hasSynced = append(c.hasSynced, r.Informer.HasSynced) // track informer syncs, prevents reconcile on empty/stale cache
		r.Informer.AddEventHandler(r.EventFuncs)
	}

}

// MakeInformerHandlers builds a cache.ResourceEventHandler that enqueues WorkItems.
// Use c.EnqueueWith(...) or c.EnqueueObjectWith(...) inside these funcs.
func MakeResourceEventHandlerFuncs(

	c BaseOperator,
	src Source,
	addEv, updEv, delEv Event,

	onAdd func(obj any),
	onUpdate func(oldObj, newObj any),
	onDelete func(obj any),
) cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if onAdd != nil {
				onAdd(obj)
			} else {
				c.EnqueueObjectWith(src, addEv, obj)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			if onUpdate != nil {
				onUpdate(oldObj, newObj)
			} else {
				c.EnqueueObjectWith(src, updEv, newObj)
			}
		},
		DeleteFunc: func(obj any) {
			if onDelete != nil {
				onDelete(obj)
			} else {
				c.EnqueueObjectWith(src, delEv, obj)
			}
		},
	}
}
