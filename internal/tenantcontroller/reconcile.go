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

	if err := c.ensureFinalizer(ctx, tenant); err != nil {
		return err
	}

	assignment, err := c.reconcileAssignments(ctx, tenant.Name, tenant.Spec.Zones)
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

func (c *Controller) reconcileDelete(ctx context.Context, tenant *seterav1.Tenant) error {
	if !slices.Contains(tenant.Finalizers, tenantFinalizer) {
		return nil
	}

	activeNodes, err := c.pods.ActiveTenantPodNodes(ctx, tenant.Name)
	if err != nil {
		return fmt.Errorf("list active pods for tenant %s: %w", tenant.Name, err)
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
		return fmt.Errorf("build tenant label for %s: %w", tenant.Name, err)
	}

	nodes, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list nodes for tenant %s cleanup: %w", tenant.Name, err)
	}

	for _, node := range nodes {
		if node.Labels[labelKey] != "true" {
			continue
		}

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
