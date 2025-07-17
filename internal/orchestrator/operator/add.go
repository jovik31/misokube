package orchestrator

import (

	//std
	"context"
	"encoding/json"
	"fmt"

	//internals
	configs "github/setera/internal"
	"github/setera/pkg/operator"

	// setera api tyes
	seterav1 "github/setera/pkg/api/setera.com/v1"

	// k8s
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	// client-go
	"k8s.io/client-go/tools/cache"
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
	if !operator.ContainsString(tenant.Finalizers, configs.TenantFinalizer) {

		t.Base.Logger.WithValues("tenant", tenant.Name, "namespace", namespace).Info("Adding finalizer to Tenant")

		// add finalizer load
		patchPayload := map[string]interface{}{
			"metadata": map[string]interface{}{
				"finalizers": []string{configs.TenantFinalizer},
			},
		}

		jsonPatch, err := json.Marshal(patchPayload)
		// patch the tenant with the finalizer
		_, err = t.Base.Seterav1Clientset.SeteraV1().Tenants(namespace).Patch(ctx, tenant.Name, types.MergePatchType, jsonPatch, metav1.PatchOptions{})
		if err != nil {
			t.Base.Logger.WithValues("tenant", tenant.Name, "namespace", namespace).Error(err, "Failed to patch Tenant with finalizer")
			t.Base.Recorder.Eventf(mod, "Warning", "PatchFailed", "Failed to patch Tenant %s with finalizer: %v", mod.Name, err)
			return fmt.Errorf("failed to patch tenant %s with finalizer: %w", mod.Name, err)
		}
		t.Base.Logger.WithValues("tenant", tenant.Name, "namespace", namespace).Info("Tenant patched with finalizer")
	}

	// list all nodestores
	nodeStores, err := t.NodestoreLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("listing NodeStores: %w", err)
	}

	// new tenant status
	seterav1TenantStatus := seterav1.TenantStatus{
		AwaitingNodeConfiguration: []string{},
		AssignedNodes:             []seterav1.NodeInfo{},
	}

	//add status to the modified tenant
	mod.Status = seterav1TenantStatus

	for _, store := range nodeStores {
		nodeID := store.Spec.Name
		mod.Status.AwaitingNodeConfiguration = append(mod.Status.AwaitingNodeConfiguration, nodeID)
	}

	// update the tenant with the finalizer and awaiting nodes
	_, err = t.Base.Seterav1Clientset.SeteraV1().Tenants(namespace).UpdateStatus(ctx, mod, metav1.UpdateOptions{})
	if err != nil {
		t.Base.Logger.WithValues("tenant", mod.Name, "namespace", namespace).Error(err, "Failed to update Tenant with finalizer and awaiting nodes")
		t.Base.Recorder.Eventf(mod, "Warning", "UpdateFailed", "Failed to update Tenant %s with finalizer and awaiting nodes: %v", mod.Name, err)
		return fmt.Errorf("failed to update tenant %s with finalizer and awaiting nodes: %w", mod.Name, err)
	}
	t.Base.Logger.WithValues("tenant", mod.Name, "namespace", namespace).Info("Tenant updated with finalizer and awaiting nodes")

	return nil
}

func (t *TenantOperator) patchPauseStatus(ctx context.Context, tenant *seterav1.Tenant, pausedStatus bool) (*seterav1.Tenant, error) {

	// best practices - always work on a copy
	tcopy := tenant.DeepCopy()
	tcopy.Status.Paused = configs.PausedTenant

	// Update the tenant status
	if _, err := t.Base.Seterav1Clientset.SeteraV1().
		Tenants(tenant.Namespace).
		UpdateStatus(ctx, tcopy, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("updating tenant status: %w", err)
	}

	t.Base.Logger.Info("updated tenant pause status", "tenant", tenant.Name, "paused", pausedStatus)

	return tcopy, nil
}
