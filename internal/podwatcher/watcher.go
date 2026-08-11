package podwatcher

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"github/setera/internal/ebpfmanager"

	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const fullSyncKey = "__setera_full_remote_pod_sync__"

type remoteDatapath interface {
	UpsertRemotePod(
		context.Context,
		ebpfmanager.RemotePod,
	) error

	DeleteRemotePod(
		context.Context,
		netip.Addr,
		string,
	) error

	ReconcileRemotePods(
		context.Context,
		[]ebpfmanager.RemotePod,
	) error
}

// Watcher translates Kubernetes Pod/Node state into remote Pod identities in
// the node-local eBPF datapath.
//
// Local Pod lifecycle is deliberately excluded: CNI + podnetwork own it.
type Watcher struct {
	logger        klog.Logger
	localNodeName string
	datapath      remoteDatapath

	podInformer cache.SharedIndexInformer
	podLister   corelisters.PodLister

	nodeInformer cache.SharedIndexInformer
	nodeLister   corelisters.NodeLister

	queue workqueue.TypedRateLimitingInterface[string]

	// known is worker-owned state keyed by namespace/name. It lets delete and
	// update reconciliation remove the exact old IP/UID after the Pod object
	// disappears or changes identity.
	known map[string]ebpfmanager.RemotePod

	ready     chan struct{}
	readyOnce sync.Once
}

func New(
	logger klog.Logger,
	localNodeName string,
	podInformer cache.SharedIndexInformer,
	podLister corelisters.PodLister,
	nodeInformer cache.SharedIndexInformer,
	nodeLister corelisters.NodeLister,
	datapath remoteDatapath,
) (*Watcher, error) {
	if localNodeName == "" {
		return nil, fmt.Errorf("podwatcher: local node name is empty")
	}
	if podInformer == nil {
		return nil, fmt.Errorf("podwatcher: Pod informer is nil")
	}
	if podLister == nil {
		return nil, fmt.Errorf("podwatcher: Pod lister is nil")
	}
	if nodeInformer == nil {
		return nil, fmt.Errorf("podwatcher: Node informer is nil")
	}
	if nodeLister == nil {
		return nil, fmt.Errorf("podwatcher: Node lister is nil")
	}
	if datapath == nil {
		return nil, fmt.Errorf("podwatcher: remote datapath is nil")
	}

	w := &Watcher{
		logger:        logger.WithName("pod-watcher"),
		localNodeName: localNodeName,
		datapath:      datapath,
		podInformer:   podInformer,
		podLister:     podLister,
		nodeInformer:  nodeInformer,
		nodeLister:    nodeLister,
		queue: workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[string](),
		),
		known: make(map[string]ebpfmanager.RemotePod),
		ready: make(chan struct{}),
	}

	w.registerEventHandlers()
	return w, nil
}

// Ready is closed after informer synchronization and the authoritative startup
// remote-Pod reconciliation both succeed.
func (w *Watcher) Ready() <-chan struct{} {
	return w.ready
}

// Run waits for both informer caches, performs an authoritative startup
// reconciliation (including stale remote BPF cleanup), then starts processing
// incremental events.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.queue.ShutDown()

	if ok := cache.WaitForCacheSync(
		ctx.Done(),
		w.podInformer.HasSynced,
		w.nodeInformer.HasSynced,
	); !ok {
		return fmt.Errorf("podwatcher: informer cache sync failed")
	}

	if err := w.reconcileAll(ctx); err != nil {
		return fmt.Errorf(
			"podwatcher: initial remote Pod reconciliation: %w",
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

	var err error
	if key == fullSyncKey {
		err = w.reconcileAll(ctx)
	} else {
		err = w.reconcileKey(ctx, key)
	}

	if err != nil {
		w.logger.Error(err, "reconcile failed", "key", key)
		w.queue.AddRateLimited(key)
		return true
	}

	w.queue.Forget(key)
	return true
}
