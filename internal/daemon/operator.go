package daemon

import (
	"context"

	// internal
	seterav1 "github/setera/pkg/api/setera.com/v1"
	seteraclient "github/setera/pkg/generated/clientset/versioned"
	seteralisters "github/setera/pkg/generated/listers/setera.com/v1"
	op "github/setera/pkg/operator"

	// k8s
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	// logging
	"k8s.io/klog/v2"
)

// Operator wires daemon-side informers and routes events to node-local handlers.
// It bridges Tenant CR changes to the dispatcher (ensure/remove) and NodeStore
// updates to Network Manager peer wiring.
type Operator struct {
	base     *op.BaseOperator
	logger   klog.Logger
	recorder record.EventRecorder

	setera seteraclient.Interface

	// tenant informer/lister
	tenantInf    cache.SharedIndexInformer
	tenantLister seteralisters.TenantLister

	// nodestore informer/lister
	nodeStoreInf    cache.SharedIndexInformer
	nodeStoreLister seteralisters.NodeStoreLister

	router *op.Router
}

func New(
	base *op.BaseOperator,
	logger klog.Logger,
	recorder record.EventRecorder,
	seteraClient seteraclient.Interface,
	tenantInformer cache.SharedIndexInformer,
	tenantLister seteralisters.TenantLister,
	nodeStoreInformer cache.SharedIndexInformer,
	nodeStoreLister seteralisters.NodeStoreLister,
) *Operator {
	o := &Operator{
		base:            base,
		logger:          logger.WithName("daemon"),
		recorder:        recorder,
		setera:          seteraClient,
		tenantInf:       tenantInformer,
		tenantLister:    tenantLister,
		nodeStoreInf:    nodeStoreInformer,
		nodeStoreLister: nodeStoreLister,
	}

	// Register handlers and track cache sync with the base operator
	o.base.AddInformerWithHandlers(o.tenantInf, cache.ResourceEventHandlerFuncs{
		AddFunc:    o.onTenantAdd,
		UpdateFunc: o.onTenantUpdate,
		DeleteFunc: o.onTenantDelete,
	})
	o.base.AddInformerWithHandlers(o.nodeStoreInf, cache.ResourceEventHandlerFuncs{
		UpdateFunc: o.onNodeStoreUpdate,
		DeleteFunc: o.onNodeStoreDelete,
	})

	// Route events to daemon-specific reconcile funcs
	o.router = op.NewRouter("daemon", map[op.Source]map[op.Event]op.ReconcileFunc{
		// Tenant CRD events
		SourceTenantCRD: {
			EventAdd:    o.reconcileTenantAdd,
			EventUpdate: o.reconcileTenantUpdate,
			EventDelete: o.reconcileTenantDelete,
		},
		// NodeStore CRD events
		SourceNodeStoreCRD: {
			EventUpdate: o.reconcileNodeStoreUpdate,
			EventDelete: o.reconcileNodeStoreDelete,
		},
	}, nil)

	return o
}

func (o *Operator) Run(ctx context.Context) error {
	o.logger.Info("starting daemon operator")
	defer o.logger.Info("daemon operator stopped")
	return o.base.Run(ctx, o.router)
}

// --- informer handlers: translate to router events ---
func (o *Operator) onTenantAdd(obj any) {
	if tenant, ok := obj.(*seterav1.Tenant); ok {
		o.logger.WithValues("event", EventAdd, "tenant", tenant.Name, "ns", tenant.Namespace).Info("enqueue tenant add")
		o.base.EnqueueObjectWith(SourceTenantCRD, EventAdd, tenant)
	}
}
func (o *Operator) onTenantUpdate(oldObj, newObj any) {
	// best-effort tombstone handling
	var newTenant *seterav1.Tenant
	switch v := newObj.(type) {
	case *seterav1.Tenant:
		newTenant = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			newTenant = vv
		}
	}
	if newTenant != nil {
		o.logger.WithValues("event", EventUpdate, "tenant", newTenant.Name, "ns", newTenant.Namespace).Info("enqueue tenant update")
		o.base.EnqueueObjectWith(SourceTenantCRD, EventUpdate, newTenant)
	}
}
func (o *Operator) onTenantDelete(obj any) {
	var tenant *seterav1.Tenant
	switch v := obj.(type) {
	case *seterav1.Tenant:
		tenant = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			tenant = vv
		}
	}
	if tenant != nil {
		o.logger.WithValues("event", EventDelete, "tenant", tenant.Name, "ns", tenant.Namespace).Info("enqueue tenant delete")
		o.base.EnqueueObjectWith(SourceTenantCRD, EventDelete, tenant)
	}
}
func (o *Operator) onNodeStoreUpdate(oldObj, newObj any) {
	var oldNS, newNS *seterav1.NodeStore
	switch v := oldObj.(type) {
	case *seterav1.NodeStore:
		oldNS = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.NodeStore); ok {
			oldNS = vv
		}
	}
	switch v := newObj.(type) {
	case *seterav1.NodeStore:
		newNS = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.NodeStore); ok {
			newNS = vv
		}
	}
	if oldNS == nil || newNS == nil {
		return
	}
	// enqueue affected tenants by name
	affected := make(map[string]struct{})
	for name := range newNS.Status.Tenants {
		affected[name] = struct{}{}
	}
	for name := range oldNS.Status.Tenants {
		affected[name] = struct{}{}
	}
	for tenantName := range affected {
		o.base.EnqueueWith(SourceNodeStoreCRD, EventUpdate, op.ResourceRef{
			Group:     "setera.com",
			Version:   "v1",
			Kind:      "Tenant",
			Namespace: newNS.Namespace,
			Name:      tenantName,
		})
	}
}
func (o *Operator) onNodeStoreDelete(obj any) {
	nodestore, ok := obj.(*seterav1.NodeStore)
	if !ok {
		return
	}
	for tenantName := range nodestore.Status.Tenants {
		o.base.EnqueueWith(SourceNodeStoreCRD, EventDelete, op.ResourceRef{
			Group:     "setera.com",
			Version:   "v1",
			Kind:      "Tenant",
			Namespace: nodestore.Namespace,
			Name:      tenantName,
		})
	}
}

// --- reconcile funcs (daemon-local, no deprecated doperator) ---
func (o *Operator) reconcileTenantAdd(ctx context.Context, src op.Source, res op.ResourceRef) error {
	return nil
}
func (o *Operator) reconcileTenantUpdate(ctx context.Context, src op.Source, res op.ResourceRef) error {
	return nil
}
func (o *Operator) reconcileTenantDelete(ctx context.Context, src op.Source, res op.ResourceRef) error {
	return nil
}

func (o *Operator) reconcileNodeStoreUpdate(ctx context.Context, src op.Source, res op.ResourceRef) error {
	return nil
}
func (o *Operator) reconcileNodeStoreDelete(ctx context.Context, src op.Source, res op.ResourceRef) error {
	return nil
}
