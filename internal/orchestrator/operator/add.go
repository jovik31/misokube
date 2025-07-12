package orchestrator

import (

	//std
	"context"
	"fmt"

	//internals
	"github/setera/pkg/operator"

	// setera api tyes
	seterav1 "github/setera/pkg/api/setera.com/v1"

	// k8s
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	// client-go
	"k8s.io/client-go/tools/cache"
)

const (
	TenantFinalizer = "finalizer.setera.com"
	PausedTenant    = true
	UnpausedTenant  = false
)

func (t *TenantOperator) addTenant(key string) error {

	ctx := context.Background()

	// split the name and namespace from the key
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		t.Base.Logger.Error(err, "Error in getting key for object", key)
		return err
	}

	//fetch tenant from cache
	tenant, err := t.TenantLister.Tenants(namespace).Get(name)
	if errors.IsNotFound(err) {
		t.Base.Logger.Error(err, "Tenant object not found in cache", key)
	} else {
		if err != nil {
			t.Base.Logger.Error(err, "Error in getting tenant object from cache", key)
			return err
		}
	}

	// ensure finalizer is present
	if !containsString(tenant.Finalizers, TenantFinalizer) {
		copy := tenant.DeepCopy()
		copy.Finalizers = append(copy.Finalizers, TenantFinalizer)
		if _, err := t.Base.Seterav1Clientset.SeteraV1().
			Tenants(namespace).
			Update(ctx, copy, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to add finalizer: %w", err)
		}
		// re-enqueue as AddEvent to continue bootstrap
		t.Base.EnqueueWithKey(operator.AddEvent, key)
		t.Base.Logger.Info("added finalizer, requeued as AddEvent", "key", key)
		return nil
	}

	// list all nodestores
	nodeStores, err := t.NodestoreLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("listing NodeStores: %w", err)
	}

	// check if there are enough nodestores for the tenant
	if len(nodeStores) < tenant.Spec.Zones {

		t.Base.Logger.Error(nil, "not enough nodestores for tenant", "tenant", tenant.Name, "zones", tenant.Spec.Zones, "available", len(nodeStores))
		if err := t.patchPauseStatus(ctx, tenant, PausedTenant); err != nil {
			return err
		}
	} else {
		t.Base.Logger.Info("enough nodestores available for tenant", "tenant", tenant.Name, "zones", tenant.Spec.Zones, "available", len(nodeStores))
		t.patchPauseStatus(ctx, tenant, UnpausedTenant)
	}

	// if we reach here, we have enough nodestores and the tenant is not paused
	t.Base.Logger.Info("tenant is ready to be processed", "tenant", tenant.Name)

	return nil
}

func (t *TenantOperator) patchPauseStatus(ctx context.Context, tenant *seterav1.Tenant, pausedStatus bool) error {

	// best practices - always work on a copy
	tcopy := tenant.DeepCopy()
	tcopy.Status.Paused = PausedTenant

	// Update the tenant status
	if _, err := t.Base.Seterav1Clientset.SeteraV1().
		Tenants(tenant.Namespace).
		UpdateStatus(ctx, tcopy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating tenant status: %w", err)
	}

	t.Base.Logger.Info("updated tenant pause status", "tenant", tenant.Name, "paused", pausedStatus)

	return nil
}

func containsString(slice []string, item string) bool {

	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
