package orchestrator

import (
	"context"
	"fmt"

	// k8s
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	// internal
	seterav1 "github/setera/pkg/api/setera.com/v1"
	op "github/setera/pkg/operator"
)

func (o *Operator) reconcileTenantAdd(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// Read from cache (treat as read-only)
	t, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if err != nil {
		return fmt.Errorf("get Tenant %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	// Ensure finalizer (metadata patch; no in-memory mutation)
	if err := o.ensureTenantFinalizer(ctx, t); err != nil {
		return err
	}

	// Seed awaiting nodes only once. Never touch once populated; never touch AssignedNodes here.
	if len(t.Status.AwaitingNodeConfiguration) > 0 || len(t.Status.AssignedNodes) > 0 {
		return nil
	}

	// Choose up to Spec.Zones nodes (order not enforced; selection may vary).
	seed, err := o.selectInitialAwaitingNodes(t)
	if err != nil {
		return err
	}
	if len(seed) == 0 {
		return nil // nothing to write
	}

	// Update status using a DeepCopy (never mutate informer object)
	mod := t.DeepCopy()
	mod.Status.AwaitingNodeConfiguration = seed

	if _, err := o.setera.SeteraV1().
		Tenants(mod.Namespace).
		UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update Tenant status %s/%s: %w", mod.Namespace, mod.Name, err)
	}

	return nil
}

// reconcileTenantUpdate handles updates to Tenant resources from tenant events.
// Only .spec.zones is expected to change (enforced by admission).
// Adjusts Status so that: len(AssignedNodes)+len(AwaitingNodeConfiguration) == spec.zones
// - If we need more nodes, append new node IDs to Awaiting from available NodeStores (not already assigned/awaiting).
// - If we need fewer nodes, remove from AssignedNodes only (never trim Awaiting).
func (o *Operator) reconcileTenantUpdate(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	t, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if err != nil {
		return fmt.Errorf("get Tenant %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	if err = o.ensureTenantFinalizer(ctx, t); err != nil {
		return err
	}

	stores, err := o.nodeStoreLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list NodeStores: %w", err)
	}

	newAwaiting, newAssigned, changed := o.recomputeAwaitingAndAssignedForZones(t, stores)
	if !changed {
		return nil
	}

	// Update status using a DeepCopy (never mutate informer object)
	mod := t.DeepCopy()
	mod.Status.AwaitingNodeConfiguration = newAwaiting
	mod.Status.AssignedNodes = newAssigned

	if _, err := o.setera.SeteraV1().
		Tenants(mod.Namespace).
		UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update Tenant status %s/%s: %w", mod.Namespace, mod.Name, err)
	}

	return nil
}
func (o *Operator) reconcileTenantDelete(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// Nothing to do on deletion; finalizer removal is handled elsewhere.
	return nil
}

func (o *Operator) reconcileNodestoreDelete(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// No-op for now
	return nil
}

func (o *Operator) reconcileNodestoreUpdate(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// Fetch tenant
	t, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if err != nil {
		return fmt.Errorf("get Tenant %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	// Use the index to get only NodeStores that reference this tenant
	var stores []*seterav1.NodeStore
	objs, idxErr := o.nodeStoreInf.GetIndexer().ByIndex(indexNodeStoreByTenant, t.Name)
	if idxErr == nil {
		stores = make([]*seterav1.NodeStore, 0, len(objs))
		for _, ojb := range objs {
			if ns, ok := ojb.(*seterav1.NodeStore); ok {
				stores = append(stores, ns)
			}
		}
	} else {
		// Fallback: if indexing fails for any reason, you can list all (less efficient).
		// stores, _ = o.nodeStoreLister.List(labels.Everything())
		return fmt.Errorf("index lookup failed for tenant %s: %w", t.Name, idxErr)
	}

	// Rebuild AssignedNodes from these NodeStores and remove them from Awaiting
	newAssigned, newAwaiting := make([]seterav1.NodeInfo, 0), make([]string, 0, len(t.Status.AwaitingNodeConfiguration))
	assignedIDs := map[string]struct{}{}
	for _, ns := range stores {
		if ns == nil || ns.Status.Tenants == nil {
			continue
		}
		ti, ok := ns.Status.Tenants[t.Name]
		if !ok {
			continue
		}
		nodeID := ns.Spec.Name
		if nodeID == "" {
			nodeID = ns.Name
		}
		assignedIDs[nodeID] = struct{}{}
		newAssigned = append(newAssigned, seterav1.NodeInfo{
			Name:       nodeID,
			NodeIP:     ns.Spec.NodeIP,
			TenantCIDR: ti.TenantCIDR,
			VtepIP:     ti.VTEP_IP,
			VtepMAC:    ti.VTEP_MAC,
		})
	}
	for _, id := range t.Status.AwaitingNodeConfiguration {
		if _, ok := assignedIDs[id]; !ok {
			newAwaiting = append(newAwaiting, id)
		}
	}

	// Idempotent write
	if equalNodeInfosByValue(t.Status.AssignedNodes, newAssigned) &&
		sameStrSlice(t.Status.AwaitingNodeConfiguration, newAwaiting) {
		return nil
	}

	mod := t.DeepCopy()
	mod.Status.AssignedNodes = newAssigned
	mod.Status.AwaitingNodeConfiguration = newAwaiting

	if _, err := o.setera.SeteraV1().Tenants(mod.Namespace).UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update Tenant status %s/%s: %w", mod.Namespace, mod.Name, err)
	}
	return nil
}

// helpers
func equalNodeInfosByValue(a, b []seterav1.NodeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]seterav1.NodeInfo, len(a))
	for _, x := range a {
		m[x.Name] = x
	}
	for _, y := range b {
		if x, ok := m[y.Name]; !ok || x != y {
			return false
		}
	}
	return true
}

func sameStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
