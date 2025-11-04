package operator

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// EventReconciler handles a WorkItem with its Source, Event, and Resource.
type EventReconciler interface {
	Name() string
	ReconcileEvent(ctx context.Context, src Source, ev Event, res ResourceRef) error
}

// ResourceFunc extracts a ResourceRef from an informer-delivered object.
type ResourceFunc func(obj any) (ResourceRef, error)

// DefaultResourceFunc tries to extract namespace/name and best-effort GVK.
func DefaultResourceFunc(obj any) (ResourceRef, error) {
	var rr ResourceRef

	// Names via meta (handles tombstones)
	if mo, ok := metaObjectFrom(obj); ok {
		rr.Name = mo.GetName()
		rr.Namespace = mo.GetNamespace()
	}

	// GVK via runtime (best-effort; informers sometimes strip it)
	if ro, ok := obj.(runtime.Object); ok && ro != nil {
		if gvk := ro.GetObjectKind().GroupVersionKind(); gvk.Kind != "" {
			rr.Group = gvk.Group
			rr.Version = gvk.Version
			rr.Kind = gvk.Kind
			return rr, nil
		}
	}

	// Fallback: type name as Kind
	t := reflect.TypeOf(obj)
	if t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t != nil {
		rr.Kind = t.Name()
	}
	return rr, nil
}

// BaseOperator is a reusable, event-aware base controller.
type BaseOperator struct {
	name     string
	logger   klog.Logger
	recorder record.EventRecorder

	queue         workqueue.TypedRateLimitingInterface[WorkItem]
	hasSynced     []cache.InformerSynced
	resourceFunc  ResourceFunc
	workers       int
	shutdownDelay time.Duration
}

// Option configures BaseOperator.
type Option func(*BaseOperator)

// WithQueue injects a custom typed queue (e.g., special rate limiter).
func WithQueue(q workqueue.TypedRateLimitingInterface[WorkItem]) Option {
	return func(c *BaseOperator) { c.queue = q }
}

// WithWorkers sets the worker parallelism (default: 1).
func WithWorkers(n int) Option {
	return func(c *BaseOperator) {
		if n > 0 {
			c.workers = n
		}
	}
}

// WithShutdownDrain lets ongoing work finish after context cancel (default: 0).
func WithShutdownDrain(d time.Duration) Option {
	return func(c *BaseOperator) { c.shutdownDelay = d }
}

// WithResourceFunc overrides the default resource extractor.
func WithResourceFunc(fn ResourceFunc) Option {
	return func(c *BaseOperator) { c.resourceFunc = fn }
}

// NewBaseOperator creates an event-aware operator.
func NewBaseOperator(name string, logger klog.Logger, recorder record.EventRecorder, opts ...Option) *BaseOperator {
	c := &BaseOperator{
		name:         name,
		logger:       logger.WithName("operator").WithValues("operator", name),
		recorder:     recorder,
		queue:        workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[WorkItem]()),
		resourceFunc: DefaultResourceFunc,
		workers:      1,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// AddInformerHandlers attaches handlers that enqueue with given source and custom events.
func (c *BaseOperator) AddInformerHandlers(inf cache.SharedIndexInformer, src Source, addEv, updEv, delEv Event) {
	c.hasSynced = append(c.hasSynced, inf.HasSynced)
	inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { c.EnqueueObjectWith(src, addEv, obj) },
		UpdateFunc: func(_, newObj any) { c.EnqueueObjectWith(src, updEv, newObj) },
		DeleteFunc: func(obj any) { c.EnqueueObjectWith(src, delEv, obj) },
	})
}

// AddInformer registers an informer and only tracks its sync (attach custom handlers separately).
func (c *BaseOperator) AddInformer(inf cache.SharedIndexInformer) {
	c.hasSynced = append(c.hasSynced, inf.HasSynced)
}

// AddInformerWithHandler attaches a custom handler and tracks sync.
func (c *BaseOperator) AddInformerWithHandler(inf cache.SharedIndexInformer, h cache.ResourceEventHandler) {
	c.hasSynced = append(c.hasSynced, inf.HasSynced)
	inf.AddEventHandler(h)
}

// AddInformerWithHandlers attaches multiple custom handlers and tracks sync.
func (c *BaseOperator) AddInformerWithHandlers(inf cache.SharedIndexInformer, hs ...cache.ResourceEventHandler) {
	c.hasSynced = append(c.hasSynced, inf.HasSynced)
	for _, h := range hs {
		if h != nil {
			inf.AddEventHandler(h)
		}
	}
}

// ResourceFor exposes the operator’s resource extractor (useful in custom handlers).
func (c *BaseOperator) ResourceFor(obj any) (ResourceRef, error) { return c.resourceFunc(obj) }

// EnqueueObjectWith enqueues an object with a specific source and event.
func (c *BaseOperator) EnqueueObjectWith(src Source, ev Event, obj any) {
	res, err := c.resourceFunc(obj)
	if err != nil {
		c.logger.Error(err, "failed to compute resource ref")
		return
	}
	// Basic guard: drop if no name (most resources should have one)
	if res.Name == "" {
		c.logger.Info("dropping event with empty resource name", "source", src, "event", ev)
		return
	}
	c.EnqueueWith(src, ev, res)
}

// EnqueueWith enqueues a resource with a specific source and event.
func (c *BaseOperator) EnqueueWith(src Source, ev Event, res ResourceRef) {
	c.queue.Add(WorkItem{Source: src, Event: ev, Resource: res})
}

// Emitter returns an Emitter view of the operator.
func (c *BaseOperator) Emitter() Emitter { return c }

// Run waits for caches to sync and starts worker goroutines that call r.ReconcileEvent.
func (c *BaseOperator) Run(ctx context.Context, r EventReconciler) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	if len(c.hasSynced) > 0 {
		if ok := cache.WaitForCacheSync(ctx.Done(), c.hasSynced...); !ok {
			return fmt.Errorf("cache sync failed for operator %q", c.name)
		}
	}

	c.logger.Info("starting workers", "workers", c.workers, "reconciler", r.Name())
	for i := 0; i < c.workers; i++ {
		go c.worker(ctx, r)
	}

	<-ctx.Done()
	if c.shutdownDelay > 0 {
		c.logger.Info("draining before shutdown", "delay", c.shutdownDelay)
		time.Sleep(c.shutdownDelay)
	}
	c.logger.Info("operator stopping", "name", c.name)
	return nil
}

func (c *BaseOperator) worker(ctx context.Context, r EventReconciler) {
	defer utilruntime.HandleCrash() // protect this goroutine from panics
	for {
		item, shutdown := c.queue.Get()
		if shutdown {
			return
		}
		if err := r.ReconcileEvent(ctx, item.Source, item.Event, item.Resource); err != nil {
			c.logger.Error(err, "reconcile failed", "source", item.Source, "event", item.Event, "resource", item.Resource, "reconciler", r.Name())
			c.queue.AddRateLimited(item)
		} else {
			c.queue.Forget(item)
		}
		c.queue.Done(item)
	}
}
