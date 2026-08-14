package tenantcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/tenantmeta"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Controller) reconcileKey(ctx context.Context, key string) error {
	tenant, err := c.tenantLister.Get(key)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get tenant %s: %w", key, err)
	}

	if tenant.DeletionTimestamp != nil {
		return c.reconcileDelete(ctx, tenant)
	}

	// Adding the finalizer changes resourceVersion. Do not continue with the
	// stale informer object: fetch the current Tenant and keep reconciling in
	// this same pass.
	if !slices.Contains(tenant.Finalizers, tenantFinalizer) {
		if err := c.ensureFinalizer(ctx, tenant); err != nil {
			return err
		}

		tenant, err = c.setera.
			SeteraV1().
			Tenants().
			Get(ctx, tenant.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf(
				"get tenant %s after adding finalizer: %w",
				key,
				err,
			)
		}
	}

	assignment, err := c.reconcileAssignments(
		ctx,
		tenant.Name,
		tenant.Spec.Zones,
	)
	if err != nil {
		return err
	}

	if err := c.updateTenantStatus(ctx, tenant, assignment); err != nil {
		return err
	}

	if assignment.reason == "ScaleDownBlocked" {
		c.queue.AddAfter(tenant.Name, blockedRetryDelay)
	}

	return nil
}

func (c *Controller) reconcileDelete(
	ctx context.Context,
	tenant *seterav1.Tenant,
) error {
	if !slices.Contains(tenant.Finalizers, tenantFinalizer) {
		return nil
	}

	activeNodes, err := c.pods.ActiveTenantPodNodes(ctx, tenant.Name)
	if err != nil {
		return fmt.Errorf(
			"list active pods for tenant %s: %w",
			tenant.Name,
			err,
		)
	}

	if len(activeNodes) != 0 {
		c.logger.Info(
			"tenant deletion waits for active pods",
			"tenant", tenant.Name,
			"nodes", len(activeNodes),
		)
		c.queue.AddAfter(tenant.Name, blockedRetryDelay)
		return nil
	}

	labelKey, err := tenantmeta.NodeTenantLabel(tenant.Name)
	if err != nil {
		return fmt.Errorf(
			"build tenant label for %s: %w",
			tenant.Name,
			err,
		)
	}

	// Deletion is uncommon and must be authoritative. Query the API directly
	// instead of depending on the Node informer cache before removing the
	// finalizer.
	nodes, err := c.kube.
		CoreV1().
		Nodes().
		List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{
				labelKey: "true",
			}).String(),
		})
	if err != nil {
		return fmt.Errorf(
			"list nodes for tenant %s cleanup: %w",
			tenant.Name,
			err,
		)
	}

	for i := range nodes.Items {
		node := &nodes.Items[i]

		if err := c.patchNodeTenantLabel(
			ctx,
			node.Name,
			labelKey,
			false,
		); err != nil {
			return err
		}
	}

	return c.removeFinalizer(ctx, tenant)
}

func (c *Controller) ensureFinalizer(
	ctx context.Context,
	tenant *seterav1.Tenant,
) error {
	if slices.Contains(tenant.Finalizers, tenantFinalizer) {
		return nil
	}

	finalizers := append(
		slices.Clone(tenant.Finalizers),
		tenantFinalizer,
	)

	return c.patchTenantFinalizers(ctx, tenant, finalizers)
}

func (c *Controller) removeFinalizer(
	ctx context.Context,
	tenant *seterav1.Tenant,
) error {
	finalizers := make([]string, 0, len(tenant.Finalizers))

	for _, finalizer := range tenant.Finalizers {
		if finalizer == tenantFinalizer {
			continue
		}

		finalizers = append(finalizers, finalizer)
	}

	return c.patchTenantFinalizers(ctx, tenant, finalizers)
}

func (c *Controller) patchTenantFinalizers(
	ctx context.Context,
	tenant *seterav1.Tenant,
	finalizers []string,
) error {
	payload := map[string]any{
		"metadata": map[string]any{
			"finalizers": finalizers,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf(
			"marshal tenant %s finalizer patch: %w",
			tenant.Name,
			err,
		)
	}

	if _, err := c.setera.
		SeteraV1().
		Tenants().
		Patch(
			ctx,
			tenant.Name,
			types.MergePatchType,
			data,
			metav1.PatchOptions{},
		); err != nil {
		return fmt.Errorf(
			"patch tenant %s finalizers: %w",
			tenant.Name,
			err,
		)
	}

	return nil
}
