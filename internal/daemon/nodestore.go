package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	seterav1 "github/setera/pkg/api/setera.com/v1"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (o *Operator) ensureNodestoreFinalizer(ctx context.Context, ns *seterav1.NodeStore) error {
	if slices.Contains(ns.Finalizers, nodestoreFinalizer) {
		return nil
	}
	finalizers := append(append([]string{}, ns.Finalizers...), nodestoreFinalizer)

	payload := map[string]any{
		"metadata": map[string]any{
			"finalizers": finalizers,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal finalizer patch for %s/%s: %w", ns.Namespace, ns.Name, err)
	}

	if _, err := o.setera.SeteraV1().
		Tenants(ns.Namespace).
		Patch(ctx, ns.Name, types.MergePatchType, b, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch finalizers for %s/%s: %w", ns.Namespace, ns.Name, err)
	}
	return nil
}
