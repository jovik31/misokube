package orchestrator

import (

	//std
	"fmt"
	"github/setera/pkg/operator"

	// setera api tyes
	"context"

	// k8s
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	// client-go
	"k8s.io/client-go/tools/cache"
)

const TenantFinalizer = "finalizer.setera.com"

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
	if len(nodeStores) == 0 {
		return t.patchPauseStatus(ctx, tenant, true, nil)
	}

	t.patchPauseStatus(ctx, tenant, false, nil)

	return nil
}
