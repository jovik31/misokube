package tenant

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type TenantOperator struct {
	nodeName string
	inf      cache.SharedIndexInformer   // informer for setera Tenants
	lister   seteraListerV1.TenantLister // optional
	queue    workqueue.RateLimitingInterface
	disp     dispatcher.Interface // Enqueue(Command), Events() not needed here
	logger   klog.Logger
}

func NewTenantOperator(nodeName string, inf cache.SharedIndexInformer, lister seteraListerV1.TenantLister, disp dispatcher.Interface) *TenantOperator {
	to := &TenantOperator{
		nodeName: nodeName,
		inf:      inf,
		lister:   lister,
		queue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "tenant-ops"),
		disp:     disp,
	}
	inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    to.onTenantEvent,
		UpdateFunc: func(oldObj, newObj interface{}) { to.onTenantEvent(newObj) },
		DeleteFunc: to.onTenantDelete,
	})
	return to
}

func (o *TenantOperator) onTenantEvent(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err == nil {
		o.queue.Add(key)
	}
}

func (o *TenantOperator) onTenantDelete(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err == nil {
		o.queue.Add(key)
	}
}

func (o *TenantOperator) Run(ctx context.Context, workers int) error {
	defer o.queue.ShutDown()
	go o.inf.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), o.inf.HasSynced) {
		return fmt.Errorf("sync timeout")
	}
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, o.worker, time.Second)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (o *TenantOperator) worker(ctx context.Context) {
	for o.processNextItem(ctx) {
	}
}

func (o *TenantOperator) processNextItem(ctx context.Context) bool {
	item, shutdown := o.queue.Get()
	if shutdown {
		return false
	}
	defer o.queue.Done(item)

	key := item.(string) // tenants are cluster-scoped; adjust if namespaced
	// Get current object from cache (handles add/update) or infer delete.
	ns, name, _ := cache.SplitMetaNamespaceKey(key)
	t, err := o.lister.Tenants(ns).Get(name) // or cluster lister if cluster-scoped
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Deleted: enqueue Remove (best-effort)
			o.enqueueRemove(name)
			o.queue.Forget(item)
			return true
		}
		o.queue.AddRateLimited(item)
		return true
	}

	// Decide Ensure/Remove for this node by checking spec.assigned contains nodeName.
	corr := fmt.Sprintf("%s:%d:%s", t.UID, t.Generation, o.nodeName)
	if assignedToThisNode(t, o.nodeName) {
		o.disp.Enqueue(dispatcher.Command{
			TenantID:      t.Name,
			Op:            dispatcher.OpEnsure,
			CorrelationID: corr,
			RequestedAt:   time.Now(),
		})
	} else {
		o.disp.Enqueue(dispatcher.Command{
			TenantID:      t.Name,
			Op:            dispatcher.OpRemove,
			CorrelationID: corr,
			RequestedAt:   time.Now(),
		})
	}

	o.queue.Forget(item)
	return true
}

func assignedToThisNode(t *seterav1.Tenant, node string) bool {
	for _, n := range t.Spec.Assigned {
		if n == node {
			return true
		}
	}
	return false
}

func (o *TenantOperator) enqueueRemove(name string) {
	o.disp.Enqueue(dispatcher.Command{
		TenantID:      name,
		Op:            dispatcher.OpRemove,
		CorrelationID: fmt.Sprintf("del:%s:%s", name, o.nodeName),
		RequestedAt:   time.Now(),
	})
}
