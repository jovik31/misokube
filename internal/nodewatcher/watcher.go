package nodewatcher

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"github/setera/internal/noderouting"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const fullSyncKey = "__setera_full_node_routing_sync__"

type routingDatapath interface {
	EnsureLocal(netip.Prefix, netip.Addr) (noderouting.LocalNode, error)
	ReconcileRemoteNodes(context.Context, []noderouting.RemoteNode) error
}

type nodeClient interface {
	Patch(
		context.Context,
		string,
		types.PatchType,
		[]byte,
		metav1.PatchOptions,
		...string,
	) (*corev1.Node, error)
}

// Watcher translates Kubernetes Node state into the node-wide VXLAN routing
// datapath. Tenant membership is deliberately ignored here.
type Watcher struct {
	logger        klog.Logger
	localNodeName string
	localPodCIDR  netip.Prefix

	nodeInformer cache.SharedIndexInformer
	nodeLister   corelisters.NodeLister
	nodeClient   nodeClient
	routing      routingDatapath

	queue workqueue.TypedRateLimitingInterface[string]

	ready     chan struct{}
	readyOnce sync.Once
}

func New(
	logger klog.Logger,
	localNodeName string,
	localPodCIDR netip.Prefix,
	nodeInformer cache.SharedIndexInformer,
	nodeLister corelisters.NodeLister,
	nodeClient nodeClient,
	routing routingDatapath,
) (*Watcher, error) {
	if localNodeName == "" {
		return nil, fmt.Errorf("nodewatcher: local node name is empty")
	}
	if !localPodCIDR.IsValid() || !localPodCIDR.Addr().Unmap().Is4() {
		return nil, fmt.Errorf("nodewatcher: invalid local IPv4 PodCIDR %s", localPodCIDR)
	}
	if nodeInformer == nil {
		return nil, fmt.Errorf("nodewatcher: Node informer is nil")
	}
	if nodeLister == nil {
		return nil, fmt.Errorf("nodewatcher: Node lister is nil")
	}
	if nodeClient == nil {
		return nil, fmt.Errorf("nodewatcher: Node client is nil")
	}
	if routing == nil {
		return nil, fmt.Errorf("nodewatcher: routing datapath is nil")
	}

	w := &Watcher{
		logger:        logger.WithName("node-watcher"),
		localNodeName: localNodeName,
		localPodCIDR:  localPodCIDR.Masked(),
		nodeInformer:  nodeInformer,
		nodeLister:    nodeLister,
		nodeClient:    nodeClient,
		routing:       routing,
		queue: workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[string](),
		),
		ready: make(chan struct{}),
	}

	w.registerEventHandlers()
	return w, nil
}

// Ready is closed after the local VTEP is created/published and the initial
// authoritative remote-node reconciliation succeeds.
func (w *Watcher) Ready() <-chan struct{} {
	return w.ready
}

func (w *Watcher) Run(ctx context.Context) error {
	defer w.queue.ShutDown()

	if ok := cache.WaitForCacheSync(ctx.Done(), w.nodeInformer.HasSynced); !ok {
		return fmt.Errorf("nodewatcher: informer cache sync failed")
	}

	localNode, err := w.nodeLister.Get(w.localNodeName)
	if err != nil {
		return fmt.Errorf("nodewatcher: get local Node %s: %w", w.localNodeName, err)
	}
	underlayIP, err := nodeInternalIPv4(localNode)
	if err != nil {
		return fmt.Errorf("nodewatcher: resolve local Node underlay: %w", err)
	}

	local, err := w.routing.EnsureLocal(w.localPodCIDR, underlayIP)
	if err != nil {
		return fmt.Errorf("nodewatcher: ensure local VTEP: %w", err)
	}
	if err := w.publishLocalVTEP(ctx, local); err != nil {
		return fmt.Errorf("nodewatcher: publish local VTEP: %w", err)
	}
	if err := w.reconcileAll(ctx); err != nil {
		return fmt.Errorf("nodewatcher: initial routing reconciliation: %w", err)
	}

	w.readyOnce.Do(func() { close(w.ready) })

	w.logger.Info(
		"starting",
		"interface", local.InterfaceName,
		"underlayIP", local.UnderlayIP.String(),
		"vtepIP", local.VTEPIP.String(),
		"vtepMAC", local.VTEPMAC.String(),
	)
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

	if err := w.reconcileAll(ctx); err != nil {
		w.logger.Error(err, "reconcile failed", "key", key)
		w.queue.AddRateLimited(key)
		return true
	}

	w.queue.Forget(key)
	return true
}