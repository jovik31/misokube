package tenantcontroller

import (
	"reflect"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

func (c *Controller) registerEventHandlers() {
	c.tenantInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onTenantAdd,
		UpdateFunc: c.onTenantUpdate,
		DeleteFunc: c.onTenantDelete,
	})

	c.nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onNodeAdd,
		UpdateFunc: c.onNodeUpdate,
		DeleteFunc: c.onNodeDelete,
	})

	c.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onPodAdd,
		UpdateFunc: c.onPodUpdate,
		DeleteFunc: c.onPodDelete,
	})
}

func (c *Controller) onTenantAdd(obj any) {
	c.enqueueTenant(obj)
}

func (c *Controller) onTenantUpdate(oldObj, newObj any) {
	oldTenant, oldOK := tenantFromObject(oldObj)
	newTenant, newOK := tenantFromObject(newObj)
	if !oldOK || !newOK {
		return
	}

	if oldTenant.Generation != newTenant.Generation ||
		!reflect.DeepEqual(oldTenant.DeletionTimestamp, newTenant.DeletionTimestamp) {
		c.enqueueTenant(newTenant)
	}
}

func (c *Controller) onTenantDelete(obj any) {
	c.enqueueTenant(obj)
}

func (c *Controller) onNodeAdd(_ any) {
	c.enqueueAllTenants()
}

func (c *Controller) onNodeUpdate(oldObj, newObj any) {
	oldNode, oldOK := nodeFromObject(oldObj)
	newNode, newOK := nodeFromObject(newObj)
	if !oldOK || !newOK {
		return
	}

	if nodeAssignmentStateChanged(oldNode, newNode) {
		c.enqueueAllTenants()
	}
}

func (c *Controller) onNodeDelete(_ any) {
	c.enqueueAllTenants()
}

func (c *Controller) onPodAdd(obj any) {
	if pod, ok := podFromObject(obj); ok {
		c.enqueuePodTenant(pod)
	}
}

func (c *Controller) onPodUpdate(oldObj, newObj any) {
	oldPod, oldOK := podFromObject(oldObj)
	newPod, newOK := podFromObject(newObj)
	if !oldOK || !newOK {
		return
	}

	oldTenant := oldPod.Labels[tenantmeta.PodTenantLabel]
	newTenant := newPod.Labels[tenantmeta.PodTenantLabel]
	if oldTenant != newTenant || oldPod.Spec.NodeName != newPod.Spec.NodeName || oldPod.Status.Phase != newPod.Status.Phase {
		if oldTenant != "" {
			c.enqueueTenantName(oldTenant)
		}
		if newTenant != "" && newTenant != oldTenant {
			c.enqueueTenantName(newTenant)
		}
	}
}

func (c *Controller) onPodDelete(obj any) {
	if pod, ok := podFromObject(obj); ok {
		c.enqueuePodTenant(pod)
	}
}

func (c *Controller) enqueueTenant(obj any) {
	tenant, ok := tenantFromObject(obj)
	if !ok {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(tenant)
	if err != nil || key == "" {
		return
	}
	c.queue.Add(key)
}

func (c *Controller) enqueueTenantName(name string) {
	if name == "" {
		return
	}

	tenant, err := c.tenantLister.Tenants("").Get(name)
	if err != nil {
		return
	}
	c.enqueueTenant(tenant)
}

func (c *Controller) enqueuePodTenant(pod *corev1.Pod) {
	if pod == nil {
		return
	}
	c.enqueueTenantName(pod.Labels[tenantmeta.PodTenantLabel])
}

func (c *Controller) enqueueAllTenants() {
	tenants, err := c.tenantLister.List(labels.Everything())
	if err != nil {
		c.logger.Error(err, "list tenants for enqueue")
		return
	}

	for _, tenant := range tenants {
		c.enqueueTenant(tenant)
	}
}

func tenantFromObject(obj any) (*seterav1.Tenant, bool) {
	switch value := obj.(type) {
	case *seterav1.Tenant:
		return value, value != nil
	case cache.DeletedFinalStateUnknown:
		tenant, ok := value.Obj.(*seterav1.Tenant)
		return tenant, ok && tenant != nil
	default:
		return nil, false
	}
}

func nodeFromObject(obj any) (*corev1.Node, bool) {
	switch value := obj.(type) {
	case *corev1.Node:
		return value, value != nil
	case cache.DeletedFinalStateUnknown:
		node, ok := value.Obj.(*corev1.Node)
		return node, ok && node != nil
	default:
		return nil, false
	}
}

func podFromObject(obj any) (*corev1.Pod, bool) {
	switch value := obj.(type) {
	case *corev1.Pod:
		return value, value != nil
	case cache.DeletedFinalStateUnknown:
		pod, ok := value.Obj.(*corev1.Pod)
		return pod, ok && pod != nil
	default:
		return nil, false
	}
}

func nodeAssignmentStateChanged(oldNode, newNode *corev1.Node) bool {
	if oldNode.Spec.Unschedulable != newNode.Spec.Unschedulable {
		return true
	}
	if nodeReady(oldNode) != nodeReady(newNode) {
		return true
	}
	if !reflect.DeepEqual(tenantmeta.Tenants(oldNode.Labels), tenantmeta.Tenants(newNode.Labels)) {
		return true
	}
	if oldNode.Labels[tenantmeta.NodeVTEPReadyLabel] != newNode.Labels[tenantmeta.NodeVTEPReadyLabel] {
		return true
	}
	if oldNode.Annotations[tenantmeta.NodeVTEPIPAnnotation] != newNode.Annotations[tenantmeta.NodeVTEPIPAnnotation] ||
		oldNode.Annotations[tenantmeta.NodeVTEPMACAnnotation] != newNode.Annotations[tenantmeta.NodeVTEPMACAnnotation] {
		return true
	}

	return nodeInternalIP(oldNode) != nodeInternalIP(newNode)
}
