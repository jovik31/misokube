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
	"k8s.io/client-go/tools/cache"
)

func (c *Controller) reconcileKey(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("split tenant key %q: %w", key, err)
	}

	tenant, err := c.tenantLister.Tenants(namespace).Get(name)
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
		c.queue.AddAfter(key, blockedRetryDelay)
	}

	return nil
}

func (c *Controller) reconcileDelete(ctx context.Context, tenant *seterav1.Tenant) error {
	if !slices.Contains(tenant.Finalizers, tenantFinalizer) {
		return nil
	}

	activeNodes, err := c.pods.ActiveTenantPodNodes(ctx, tenant.Name)
	if err != nil {
		return err
	}
	if len(activeNodes) != 0 {
		c.logger.Info("tenant deletion waits for active pods", "tenant", tenant.Name, "nodes", len(activeNodes))
		key, keyErr := cache.MetaNamespaceKeyFunc(tenant)
		if keyErr == nil && key != "" {
			c.queue.AddAfter(key, blockedRetryDelay)
		}
		return nil
	}

	labelKey, err := tenantmeta.NodeTenantLabel(tenant.Name)
	if err != nil {
		return err
	}

	nodes, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list nodes for tenant cleanup: %w", err)
	}
	for _, node := range nodes {
		if node.Labels[labelKey] != "true" {
			continue
		}
		if err := c.patchNodeTenantLabel(ctx, node.Name, labelKey, false); err != nil {
			return err
		}
	}

	return c.removeFinalizer(ctx, tenant)
}

func (c *Controller) ensureFinalizer(ctx context.Context, tenant *seterav1.Tenant) error {
	if slices.Contains(tenant.Finalizers, tenantFinalizer) {
		return nil
	}

	finalizers := append(slices.Clone(tenant.Finalizers), tenantFinalizer)
	return c.patchTenantFinalizers(ctx, tenant, finalizers)
}

func (c *Controller) removeFinalizer(ctx context.Context, tenant *seterav1.Tenant) error {
	finalizers := make([]string, 0, len(tenant.Finalizers))
	for _, finalizer := range tenant.Finalizers {
		if finalizer != tenantFinalizer {
			finalizers = append(finalizers, finalizer)
		}
	}
	return c.patchTenantFinalizers(ctx, tenant, finalizers)
}

func (c *Controller) patchTenantFinalizers(ctx context.Context, tenant *seterav1.Tenant, finalizers []string) error {
	payload := map[string]any{
		"metadata": map[string]any{
			"finalizers": finalizers,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal tenant finalizer patch: %w", err)
	}

	if _, err := c.setera.SeteraV1().Tenants(tenant.Namespace).Patch(
		ctx,
		tenant.Name,
		types.MergePatchType,
		data,
		metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("patch tenant %s finalizers: %w", tenant.Name, err)
	}
	return nil
}
