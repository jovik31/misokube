package k8s

import (
	"context"
	"fmt"
	"log"
	"time"

	"k8s.io/client-go/dynamic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var tenantGVR = schema.GroupVersionResource{
	Group:    "misokube.com",
	Version:  "v1",
	Resource: "tenants",
}

func EnsureTenant(
	ctx context.Context,
	dynClient dynamic.Interface,
	name string,
	zones int,
) error {
	tenant := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "misokube.com/v1",
			"kind":       "Tenant",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"zones": int64(zones),
			},
		},
	}

	resource := dynClient.Resource(tenantGVR)
	current, err := resource.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := resource.Create(ctx, tenant, metav1.CreateOptions{}); err != nil {
			return err
		}
		log.Printf("created Tenant %q with %d zones", name, zones)
		return nil
	}
	if err != nil {
		return err
	}

	currentZones, found, err := unstructured.NestedInt64(current.Object, "spec", "zones")
	if err != nil {
		return fmt.Errorf("read Tenant %q zones: %w", name, err)
	}
	if found && currentZones == int64(zones) {
		return nil
	}
	if err := unstructured.SetNestedField(current.Object, int64(zones), "spec", "zones"); err != nil {
		return fmt.Errorf("set Tenant %q zones: %w", name, err)
	}
	if _, err := resource.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		return err
	}
	log.Printf("updated Tenant %q to %d zones", name, zones)
	return nil
}

func WaitTenantAssigned(
	ctx context.Context,
	dynClient dynamic.Interface,
	name string,
	timeout time.Duration,
) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		tenant, err := dynClient.Resource(tenantGVR).Get(ctx, name, metav1.GetOptions{})
		if err == nil && tenantAssigned(tenant) {
			return nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for Tenant %q to become assigned", name)
		case <-ticker.C:
		}
	}
}

func tenantAssigned(tenant *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(tenant.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	generation := tenant.GetGeneration()
	for _, item := range conditions {
		condition, ok := item.(map[string]interface{})
		if !ok || condition["type"] != "Assigned" || condition["status"] != "True" {
			continue
		}
		observed, ok := condition["observedGeneration"].(int64)
		return !ok || observed == generation
	}

	return false
}
