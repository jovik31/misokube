package orchestrator

import (
	"context"
	"fmt"
	"slices"

	// k8s
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	// internal
	seterav1 "github/setera/pkg/api/setera.com/v1"
	op "github/setera/pkg/operator"
)

// ----- these reconcile funcs handle NodeStore CRD events ----- for tenant objects
func (o *Operator) reconcileNodestoreAdd(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	o.logger.Info("reconcileNodestoreAdd called", "tenant", fmt.Sprintf("%s/%s", ref.Namespace, ref.Name))
	return o.reconcileNodestoreUpdate(ctx, SourceNodeStoreCRD, ref)
}

func (o *Operator) reconcileNodestoreDelete(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	// if a nodestore is deleted, its tenant assignments are removed so we need to remove the nodestore's nodes from the tenants' AssignedNodes lists

	// Fetch tenant
	t, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if err != nil {
		return fmt.Errorf("get Tenant %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	// Rebuild AssignedNodes without the deleted nodestore's node
	newAssigned := make([]seterav1.NodeInfo, 0)
	for _, ni := range t.Status.AssignedNodes {
		if ni.Name != ref.Name { // ref.Name is the nodestore name
			newAssigned = append(newAssigned, ni)
		}
	}

	// Idempotent write
	if equalNodeInfosByValue(t.Status.AssignedNodes, newAssigned) {
		return nil
	}

	mod := t.DeepCopy()
	mod.Status.AssignedNodes = newAssigned

	if _, err := o.setera.SeteraV1().Tenants(mod.Namespace).UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("delete tenant status from nodestore events %s/%s: %w", mod.Namespace, mod.Name, err)
	}

	return nil

}

// a tenant is enqueued for nodestore updates when one of its nodestores is updated
// this reconcile func recomputes the tenant's AssignedNodes and AwaitingNodeConfiguration based on current nodestore state
func (o *Operator) reconcileNodestoreUpdate(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	o.logger.Info("reconcileNodestoreUpdate called", "tenant", fmt.Sprintf("%s/%s", ref.Namespace, ref.Name))

	//get the tenant
	t, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if err != nil {
		return fmt.Errorf("get Tenant %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	if t == nil {
		o.logger.Info("tenant not found; skipping nodestore update reconcile", "tenant", fmt.Sprintf("%s/%s", ref.Namespace, ref.Name))
		return nil
	}

	// fetch nodestore from indexer
	nodestores, err := o.nodeStoresForTenant(t.Name)
	if err != nil {
		return fmt.Errorf("failed to retrieve nodestores from indexer %s: %w", t.Name, err)
	}

	newAssigned := make([]seterav1.NodeInfo, 0)
	for _, ns := range nodestores {

		tinfra, ok := ns.Status.Tenants[t.Name]
		if ok {
			newAssigned = append(newAssigned, seterav1.NodeInfo{
				Name:       ns.Spec.Name,
				NodeIP:     ns.Spec.NodeIP,
				TenantCIDR: tinfra.TenantCIDR,
				VtepIP:     tinfra.VTEP_IP,
				VtepMAC:    tinfra.VTEP_MAC,
			})
		}

	}

	// Backfill awaiting nodes up to zones using all available NodeStores
	allStores, err := o.nodeStoreLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list NodeStores: %w", err)
	}
	temp := t.DeepCopy()
	temp.Status.AssignedNodes = newAssigned
	newAwaiting, newAssignedFinal, changedStructural := o.recomputeAwaitingAndAssignedForZones(temp, allStores)

	// detect pure value changes (e.g., TenantCIDR) even when counts stay the same
	awaitingChanged := !slices.Equal(t.Status.AwaitingNodeConfiguration, newAwaiting)
	assignedChanged := !equalNodeInfosByValue(t.Status.AssignedNodes, newAssignedFinal)
	if !changedStructural && !awaitingChanged && !assignedChanged {
		return nil
	}

	// Update status using a DeepCopy (never mutate informer object)
	mod := t.DeepCopy(), status
	
	mod.Status.AwaitingNodeConfiguration = newAwaiting
	mod.Status.AssignedNodes = newAssignedFinal

	if _, err := o.setera.SeteraV1().
		Tenants(mod.Namespace).
		UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update Tenant status from nodestore source event %s/%s: %w", mod.Namespace, mod.Name, err)
	}

	// After persisting status, if enough nodes exist but not all zones are assigned, return error to re-enqueue
	zones := t.Spec.Zones
	if len(allStores) >= zones && len(newAssignedFinal) != zones {
		return fmt.Errorf("awaiting full zone assignment: zones=%d assigned=%d availableStores=%d", zones, len(newAssignedFinal), len(allStores))
	}

	return nil
}

func (o *Operator) nodeStoresForTenant(tenantName string) ([]*seterav1.NodeStore, error) {
	objs, err := o.nodeStoreInf.GetIndexer().ByIndex(indexNodeStoreByTenant, tenantName)
	if err != nil {
		return nil, err
	}
	stores := make([]*seterav1.NodeStore, 0, len(objs))
	for _, obj := range objs {
		ns, ok := obj.(*seterav1.NodeStore)
		if !ok || ns == nil {
			continue
		}
		stores = append(stores, ns)
	}
	return stores, nil
}
