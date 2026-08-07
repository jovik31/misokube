package orchestrator

import (
	"context"
	"fmt"
	"slices"

	// k8s
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	// internal
	op "github/setera/pkg/operator"
)

func (o *Operator) reconcileTenantAdd(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// Read from cache (treat as read-only)
	t, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if err != nil {
		return fmt.Errorf("get Tenant %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	if t.DeletionTimestamp != nil {
		return o.reconcileTenantDelete(ctx, SourceTenantCRD, ref)
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
		return fmt.Errorf("add tenant status from tenant source event%s/%s: %w", mod.Namespace, mod.Name, err)
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
	if t.DeletionTimestamp != nil {
		return o.reconcileTenantDelete(ctx, SourceTenantCRD, ref)
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
		return fmt.Errorf("update Tenant status from tenant source event %s/%s: %w", mod.Namespace, mod.Name, err)
	}

	return nil
}
func (o *Operator) reconcileTenantDelete(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	t, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get deleting Tenant %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	if !slices.Contains(t.Finalizers, tenantFinalizer) {
		return nil
	}
	stores, err := o.nodeStoresForTenant(t.Name)
	if err != nil {
		return fmt.Errorf("list NodeStores for deleting Tenant %s/%s: %w", t.Namespace, t.Name, err)
	}
	if len(stores) > 0 {
		nodeNames := make([]string, 0, len(stores))
		for _, store := range stores {
			nodeName := store.Spec.Name
			if nodeName == "" {
				nodeName = store.Name
			}
			nodeNames = append(nodeNames, nodeName)
		}
		return fmt.Errorf("waiting for Tenant %s/%s cleanup on NodeStores %v", t.Namespace, t.Name, nodeNames)
	}
	o.logger.WithValues("tenant", t.Name, "namespace", t.Namespace).Info("tenant cleanup completed; removing finalizer")
	return o.removeTenantFinalizer(ctx, t)
}
