package daemon

import (
	"context"

	nmanager "github/setera/internal/nmanager"
	seteraclient "github/setera/pkg/generated/clientset/versioned"
	seteralisters "github/setera/pkg/generated/listers/setera.com/v1"
	op "github/setera/pkg/operator"

	// k8s
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	// logging
	"k8s.io/klog/v2"
)

// Operator wires daemon-side informers and routes events to node-local handlers.
// It bridges Tenant CR changes to the dispatcher (ensure/remove) and NodeStore
// updates to Network Manager peer wiring.
type Operator struct {
	nodeName string

	base     *op.BaseOperator
	logger   klog.Logger
	recorder record.EventRecorder

	setera     seteraclient.Interface
	kubeclient *kubernetes.Clientset

	// dispatcher for tenant-level ensure/remove operations (injected)
	dp Dispatcher

	// nmOps provides snapshots for NodeStore mirroring
	nmOps nmanager.NodestoreOps

	// tenant informer/lister
	tenantInf    cache.SharedIndexInformer
	tenantLister seteralisters.TenantLister

	// nodestore informer/lister
	nodeStoreInf    cache.SharedIndexInformer
	nodeStoreLister seteralisters.NodeStoreLister

	router op.EventReconciler
}

func New(
	base *op.BaseOperator,
	logger klog.Logger,
	recorder record.EventRecorder,
	seteraClient seteraclient.Interface,
	kubeclient *kubernetes.Clientset,
	tenantInformer cache.SharedIndexInformer,
	tenantLister seteralisters.TenantLister,
	nodeStoreInformer cache.SharedIndexInformer,
	nodeStoreLister seteralisters.NodeStoreLister,
	dispatcher Dispatcher,
) *Operator {
	o := &Operator{
		base:            base,
		logger:          logger.WithName("daemon"),
		recorder:        recorder,
		setera:          seteraClient,
		kubeclient:      kubeclient,
		tenantInf:       tenantInformer,
		tenantLister:    tenantLister,
		nodeStoreInf:    nodeStoreInformer,
		nodeStoreLister: nodeStoreLister,
		dp:              dispatcher,
		nmOps:           nil,
	}

	// Register handlers and track cache sync with the base operator
	o.base.AddInformerWithHandlers(o.tenantInf, cache.ResourceEventHandlerFuncs{

		AddFunc:    o.addEventTenantHandler,
		UpdateFunc: o.updateEventTenantHandler, // each daemon only acts on updates and deletes of tenant objects
		DeleteFunc: o.deleteEventTenantHandler,
	})
	o.base.AddInformerWithHandlers(o.nodeStoreInf, cache.ResourceEventHandlerFuncs{
		AddFunc:    o.addNodestoreEventHandler,
		UpdateFunc: o.updateNodestoreEventHandler,
		DeleteFunc: o.deleteEventNodestoretHandler,
	})

	// Route events to daemon-specific reconcile funcs
	o.router = op.NewRouter("daemon", map[op.Source]map[op.Event]op.ReconcileFunc{
		// Tenant CRD events
		SourceTenantCRD: {
			EventAdd:    o.reconcileTenantAddUpdate,
			EventUpdate: o.reconcileTenantAddUpdate,
			EventDelete: o.reconcileTenantDelete,
		},
		// NodeStore CRD events
		SourceNodeStoreCRD: {
			EventAdd:    o.reconcileNodeStoreAdd,
			EventUpdate: o.reconcileNodeStoreUpdate,
			EventDelete: o.reconcileNodeStoreDelete,
		},

		// Network Manager events. these are triggered by changes in the tenants node infrastructure and network assignments
		SourceNetworkManager: {
			EventAdd:    o.reconcileNodestoreTenantAdd,
			EventUpdate: o.reconcileNodestoreTenantUpdate,
			EventDelete: o.reconcileNodestoreTenantDelete,
		},
	}, nil)

	return o
}

// SetNodeName sets the local node name used for NodeStore selection.
func (o *Operator) SetNodeName(name string) { o.nodeName = name }

// SetNMOps injects the NetworkManager ops provider for snapshots.
func (o *Operator) SetNMOps(nm nmanager.NodestoreOps) { o.nmOps = nm }

// removed SetDispatcher; dispatcher is injected via New()

func (o *Operator) Run(ctx context.Context) error {
	o.logger.Info("starting daemon operator")
	defer o.logger.Info("daemon operator stopped")
	return o.base.Run(ctx, o.router)
}

func (o *Operator) ensurePeer(ctx context.Context, tenantID string, remote nmanager.RemoteTenantInfra) {
	if tenantID == "" {
		return
	}
	if o.dp != nil {
		o.dp.EnsurePeer(remote, tenantID)
		return
	}
	if o.nmOps != nil {
		if err := o.nmOps.EnsurePeer(ctx, tenantID, remote); err != nil {
			o.logger.WithValues("tenant", tenantID, "remote", remote.NodeName).Info("ensure peer direct call failed", "err", err)
		}
	}
}

func (o *Operator) removePeer(ctx context.Context, tenantID string, remote nmanager.RemoteTenantInfra) {
	if tenantID == "" {
		return
	}
	if o.dp != nil {
		o.dp.RemovePeer(remote, tenantID)
		return
	}
	if o.nmOps != nil {
		if err := o.nmOps.RemovePeer(ctx, tenantID, remote); err != nil {
			o.logger.WithValues("tenant", tenantID, "remote", remote.NodeName).Info("remove peer direct call failed", "err", err)
		}
	}
}
