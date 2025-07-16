package orchestrator

import (

	//std
	"context"
	"fmt"

	//internals
	configs "github/setera/internal"

	// setera api tyes
	seterav1 "github/setera/pkg/api/setera.com/v1"

	// k8s
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	// client-go
	"k8s.io/client-go/tools/cache"
)

func (t *TenantOperator) addTenant(key string) error {

	//ctx := context.Background()

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
		return fmt.Errorf("tenant %s not found in namespace %s", name, namespace)
	} else {
		if err != nil {
			t.Base.Logger.Error(err, "Error in getting tenant object from cache", key)
			return err
		}
	}

	// ------------------------------------------------------------------------------------------//

	// create two copies
	//og := tenant.DeepCopy()
	mod := tenant.DeepCopy()

	// ensure finalizer is present
	if !ContainsString(tenant.Finalizers, configs.TenantFinalizer) {
		mod.Finalizers = append(mod.Finalizers, configs.TenantFinalizer)
		t.Base.Logger.Info("adding finalizer to tenant", "tenant", tenant.Name, "namespace", tenant.Namespace)
	}

	// list all nodestores
	nodeStores, err := t.NodestoreLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("listing NodeStores: %w", err)
	}

	// correct score assignement
	/*scores := t.ScoreCache.GetWithCriteria(og.Spec.Zones)

	if len(scores) < og.Spec.Zones {
		t.Base.Logger.Error(nil, "not enough scores for tenant", "tenant", og.Name, "zones", og.Spec.Zones, "available", len(scores))
		mod.Status.Paused = PausedTenant
	} else {
		waitingNodes := make([]string, len(scores))
		for i, score := range scores {
	}*/

	// assign nodestores to the tenant
	for _, store := range nodeStores {
		nodeID := store.Spec.Name
		mod.Status.AwaitingNodeConfiguration = append(mod.Status.AwaitingNodeConfiguration, nodeID)
	}

	return nil
}

func (t *TenantOperator) patchPauseStatus(ctx context.Context, tenant *seterav1.Tenant, pausedStatus bool) (*seterav1.Tenant, error) {

	// best practices - always work on a copy
	tcopy := tenant.DeepCopy()
	tcopy.Status.Paused = PausedTenant

	// Update the tenant status
	if _, err := t.Base.Seterav1Clientset.SeteraV1().
		Tenants(tenant.Namespace).
		UpdateStatus(ctx, tcopy, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("updating tenant status: %w", err)
	}

	t.Base.Logger.Info("updated tenant pause status", "tenant", tenant.Name, "paused", pausedStatus)

	return tcopy, nil
}
