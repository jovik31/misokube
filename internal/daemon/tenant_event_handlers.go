package daemon

import (
	"os"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	op "github/setera/pkg/operator"

	"k8s.io/client-go/tools/cache"
)

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
	if tenantAwaitingContains(t, nodeName) {
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
	var oldT, newT *seterav1.Tenant
	switch v := oldObj.(type) {
	case *seterav1.Tenant:
		oldT = v
	case cache.DeletedFinalStateUnknown:
		if vv, ok := v.Obj.(*seterav1.Tenant); ok {
			oldT = vv
		}
	}
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
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		return
	}
	wasAwaiting := tenantAwaitingContains(oldT, nodeName)
	nowAwaiting := tenantAwaitingContains(newT, nodeName)
	if nowAwaiting && !wasAwaiting {
		o.base.EnqueueWith(SourceTenantCRD, EventUpdate, op.ResourceRef{
			Group:     "setera.com",
			Version:   "v1",
			Kind:      "Tenant",
			Namespace: newT.Namespace,
			Name:      newT.Name,
		})
	}
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

func tenantAwaitingContains(t *seterav1.Tenant, node string) bool {
	if t == nil {
		return false
	}
	for _, n := range t.Status.AwaitingNodeConfiguration {
		if n == node {
			return true
		}
	}
	return false
}
