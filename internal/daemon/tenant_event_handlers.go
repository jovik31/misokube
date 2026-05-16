package daemon

import (
	"os"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	op "github/setera/pkg/operator"

	"k8s.io/client-go/tools/cache"
)

// enqueues a reconcile when the local node appears in the tenant's AwaitingNodeConfiguration set.
func (o *Operator) addEventTenantHandler(obj interface{}) {
	var t *seterav1.Tenant
	switch v := obj.(type) {
	case *seterav1.Tenant:
		t = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			t = vv
		}
	}
	if t == nil {
		return
	}
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		return
	}
	if o.tenantAwaitingContains(t, nodeName) {
		o.base.EnqueueWith(SourceTenantCRD, EventAdd, op.ResourceRef{
			Group:     "setera.com",
			Version:   "v1",
			Kind:      "Tenant",
			Namespace: t.Namespace,
			Name:      t.Name,
		})
	}
}

// updateEventTenantHandler enqueues a reconcile when the local node newly
// appears in the tenant's AwaitingNodeConfiguration set.
func (o *Operator) updateEventTenantHandler(oldObj, newObj interface{}) {

	o.logger.Info("Tenant update event received")
	var newT *seterav1.Tenant
	switch v := newObj.(type) {
	case *seterav1.Tenant:
		newT = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			newT = vv
		}
	}
	if newT == nil {
		return
	}
	o.base.EnqueueWith(SourceTenantCRD, EventUpdate, op.ResourceRef{
		Group:     "setera.com",
		Version:   "v1",
		Kind:      "Tenant",
		Namespace: newT.Namespace,
		Name:      newT.Name,
	})
}

// deleteEventTenantHandler always enqueues remove handling for this tenant.
func (o *Operator) deleteEventTenantHandler(obj interface{}) {
	var t *seterav1.Tenant
	switch v := obj.(type) {
	case *seterav1.Tenant:
		t = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			t = vv
		}
	}
	if t == nil {
		return
	}
	o.base.EnqueueWith(SourceTenantCRD, EventDelete, op.ResourceRef{
		Group:     "setera.com",
		Version:   "v1",
		Kind:      "Tenant",
		Namespace: t.Namespace,
		Name:      t.Name,
	})
}
