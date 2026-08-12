package servicewatcher

import (
	"context"
	"fmt"
	"sync"

	seteraebpf "github/setera/pkg/ebpf"

	corelisters "k8s.io/client-go/listers/core/v1"
	discoverylisters "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const fullSyncKey = "__setera_full_service_sync__"

type serviceDatapath interface {
	ReconcileServices(
		context.Context,
		[]seteraebpf.Service,
	) error
}

// Watcher translates Kubernetes Service and EndpointSlice state into the
// node-local eBPF Service maps.
//
// These maps are control-plane state only in this phase. kube-proxy remains the
// active Service datapath until Setera's socket and packet load balancers use
// them.
type Watcher struct {
	logger   klog.Logger
	datapath serviceDatapath

	serviceInformer cache.SharedIndexInformer
	serviceLister   corelisters.ServiceLister

	endpointSliceInformer cache.SharedIndexInformer
	endpointSliceLister   discoverylisters.EndpointSliceLister

	podInformer cache.SharedIndexInformer
	podLister   corelisters.PodLister

	queue workqueue.TypedRateLimitingInterface[string]

	ready     chan struct{}
	readyOnce sync.Once
}

func New(
	logger klog.Logger,
	serviceInformer cache.SharedIndexInformer,
	serviceLister corelisters.ServiceLister,
	endpointSliceInformer cache.SharedIndexInformer,
	endpointSliceLister discoverylisters.EndpointSliceLister,
	podInformer cache.SharedIndexInformer,
	podLister corelisters.PodLister,
	datapath serviceDatapath,
) (*Watcher, error) {
	if serviceInformer == nil {
		return nil, fmt.Errorf("servicewatcher: Service informer is nil")
	}
	if serviceLister == nil {
		return nil, fmt.Errorf("servicewatcher: Service lister is nil")
	}
	if endpointSliceInformer == nil {
		return nil, fmt.Errorf("servicewatcher: EndpointSlice informer is nil")
	}
	if endpointSliceLister == nil {
		return nil, fmt.Errorf("servicewatcher: EndpointSlice lister is nil")
	}
	if podInformer == nil {
		return nil, fmt.Errorf("servicewatcher: Pod informer is nil")
	}
	if podLister == nil {
		return nil, fmt.Errorf("servicewatcher: Pod lister is nil")
	}
	if datapath == nil {
		return nil, fmt.Errorf("servicewatcher: Service datapath is nil")
	}

	w := &Watcher{
		logger:                logger.WithName("service-watcher"),
		datapath:              datapath,
		serviceInformer:       serviceInformer,
		serviceLister:         serviceLister,
		endpointSliceInformer: endpointSliceInformer,
		endpointSliceLister:   endpointSliceLister,
		podInformer:           podInformer,
		podLister:             podLister,
		queue: workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[string](),
		),
		ready: make(chan struct{}),
	}

	w.registerEventHandlers()
	return w, nil
}

// Ready is closed after informer synchronization and the authoritative initial
// Service reconciliation succeed.
func (w *Watcher) Ready() <-chan struct{} {
	return w.ready
}

// Run synchronizes informer caches, rebuilds the Service maps from Kubernetes,
// then processes Service, EndpointSlice, and relevant Pod changes.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.queue.ShutDown()

	if ok := cache.WaitForCacheSync(
		ctx.Done(),
		w.serviceInformer.HasSynced,
		w.endpointSliceInformer.HasSynced,
		w.podInformer.HasSynced,
	); !ok {
		return fmt.Errorf("servicewatcher: informer cache sync failed")
	}

	if err := w.reconcileAll(ctx); err != nil {
		return fmt.Errorf(
			"servicewatcher: initial Service reconciliation: %w",
			err,
		)
	}

	w.readyOnce.Do(func() { close(w.ready) })

	w.logger.Info("starting")
	defer w.logger.Info("stopped")

	go w.worker(ctx)

	<-ctx.Done()
	return nil
}

func (w *Watcher) worker(ctx context.Context) {
	for w.processNext(ctx) {
	}
}

func (w *Watcher) processNext(ctx context.Context) bool {
	key, shutdown := w.queue.Get()
	if shutdown {
		return false
	}
	defer w.queue.Done(key)

	if key != fullSyncKey {
		w.queue.Forget(key)
		return true
	}

	if err := w.reconcileAll(ctx); err != nil {
		w.logger.Error(err, "Service reconciliation failed")
		w.queue.AddRateLimited(fullSyncKey)
		return true
	}

	w.queue.Forget(fullSyncKey)
	return true
}
