package orchestrator

import (
	"context"

	// internal
	seteraclient "github/setera/pkg/generated/clientset/versioned"
	seteralisters "github/setera/pkg/generated/listers/setera.com/v1"
	op "github/setera/pkg/operator"

	// client-go
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	// logging
	"k8s.io/klog/v2"
)

type Operator struct {
	base     *op.BaseOperator
	logger   klog.Logger
	recorder record.EventRecorder

	setera seteraclient.Interface

	// tenant lister and informer
	tenantInf    cache.SharedIndexInformer
	tenantLister seteralisters.TenantLister

	// nodestore lister and informer
	nodeStoreInf    cache.SharedIndexInformer
	nodeStoreLister seteralisters.NodeStoreLister

	router op.EventReconciler
}

func New(

	// generic operator
	base *op.BaseOperator,

	// log solution
	logger klog.Logger,

	// record events
	recorder record.EventRecorder,

	// setera client
	seteraClient seteraclient.Interface,

	// crd informers and listers

	// tenant
	tenantInformer cache.SharedIndexInformer,
	tenantLister seteralisters.TenantLister,

	// nodestore
	nodeStoreInformer cache.SharedIndexInformer,
	nodeStoreLister seteralisters.NodeStoreLister,

) *Operator {

	o := &Operator{
		base:            base,
		logger:          logger.WithName("orchestrator"),
		recorder:        recorder,
		setera:          seteraClient,
		tenantInf:       tenantInformer,
		tenantLister:    tenantLister,
		nodeStoreInf:    nodeStoreInformer,
		nodeStoreLister: nodeStoreLister,
	}

	// Add informer indexes BEFORE registering handlers / starting informers
	if err := o.nodeStoreInf.AddIndexers(cache.Indexers{
		indexNodeStoreByTenant: indexNodestoreByTenant,
	}); err != nil {
		o.logger.Error(err, "failed to add nodestore informer indexes")
		return nil
	}

	// Register handlers and track cache sync with the base operator
	o.base.AddInformerWithHandlers(o.tenantInf, cache.ResourceEventHandlerFuncs{
		AddFunc:    o.addEventTenantHandler,
		UpdateFunc: o.updateEventTenantHandler,
		DeleteFunc: o.deleteEventTenantHandler,
	})
	o.base.AddInformerWithHandlers(o.nodeStoreInf, cache.ResourceEventHandlerFuncs{

		AddFunc:    o.addEventNodestoreHandler,
		UpdateFunc: o.updateEventNodestoreHandler,
		DeleteFunc: o.deleteEventNodestoreHandler,
	})

	o.router = op.NewRouter("orchestrator", map[op.Source]map[op.Event]op.ReconcileFunc{

		SourceTenantCRD: {
			EventAdd:    o.reconcileTenantAdd,
			EventUpdate: o.reconcileTenantUpdate,
			EventDelete: o.reconcileTenantDelete,
		},
		SourceNodeStoreCRD: {
			EventUpdate: o.reconcileNodestoreUpdate,
			EventDelete: o.reconcileNodestoreDelete,
		},
	}, nil)

	return o
}

func (o *Operator) Run(ctx context.Context) error {
	o.logger.Info("starting orchestrator")
	defer o.logger.Info("orchestrator stopped")
	return o.base.Run(ctx, o.router)
}
