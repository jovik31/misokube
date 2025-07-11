package orchestrator

import (
	"context"
	"fmt"

	seterav1 "github/setera/pkg/api/setera.com/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
)

// addTenant handles AddEvents for Tenant CRs.
/*func (t *TenantOperator) addTenant(key string) error {
	ctx := context.Background()

	// 1) split namespace/name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		t.Base.Logger.Error(err, "invalid key", "key", key)
		return err
	}

	// 2) fetch Tenant from cache
	tenant, err := t.TenantLister.Tenants(namespace).Get(name)
	if errors.IsNotFound(err) {
		t.Base.Logger.Info("tenant not found, skipping", "key", key)
		return nil
	} else if err != nil {
		t.Base.Logger.Error(err, "error fetching tenant", "key", key)
		return err
	}

	// 3) deletion check & finalizer removal
	if !tenant.DeletionTimestamp.IsZero() {
		if containsString(tenant.Finalizers, TenantFinalizer) {
			copy := tenant.DeepCopy()
			copy.Finalizers = removeString(copy.Finalizers, TenantFinalizer)
			if _, err := t.Base.Seterav1Clientset.SeteraV1().
				Tenants(namespace).
				Update(ctx, copy, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("removing finalizer: %w", err)
			}
		}
		return nil
	}

	// 4) ensure finalizer is present
	if !containsString(tenant.Finalizers, TenantFinalizer) {
		copy := tenant.DeepCopy()
		copy.Finalizers = append(copy.Finalizers, TenantFinalizer)
		if _, err := t.Base.Seterav1Clientset.SeteraV1().
			Tenants(namespace).
			Update(ctx, copy, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("adding finalizer: %w", err)
		}
		// requeue so we pick it up with finalizer set
		return fmt.Errorf("finalizer added, requeueing")
	}

	// 5) list NodeStores
	nodeStores, err := t.NodestoreLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("listing NodeStores: %w", err)
	}
	if len(nodeStores) == 0 {
		// no nodes yet—pause
		return t.patchPauseStatus(ctx, tenant, true, []string{})
	}

	// 6) collect scores & track missing
	type scored struct {
		id    string
		score int
	}
	var scoredNodes []scored
	var missingIDs []string

	for _, ns := range nodeStores {
		id := ns.Spec.NodeID
		sc, ok := t.ScoreCache.Get(id)
		if !ok {
			missingIDs = append(missingIDs, id)
		} else {
			scoredNodes = append(scoredNodes, scored{id: id, score: sc})
		}
	}

	if len(missingIDs) > 0 {
		// pause until all scores arrive
		return t.patchPauseStatus(ctx, tenant, true, missingIDs)
	}

	// 7) unpause if needed
	if tenant.Status.Paused || len(tenant.Status.WaitingForNodeConfiguration) > 0 {
		if err := t.patchPauseStatus(ctx, tenant, false, nil); err != nil {
			return err
		}
	}

	// 8) pick top-Zones by score
	zones := tenant.Spec.Zones
	sort.Slice(scoredNodes, func(i, j int) bool {
		return scoredNodes[i].score > scoredNodes[j].score
	})
	if zones > len(scoredNodes) {
		zones = len(scoredNodes)
	}
	chosen := make([]string, zones)
	for i := 0; i < zones; i++ {
		chosen[i] = scoredNodes[i].id
	}

	// 9) update status.assignedNodes if changed
	if !equalStringSlices(chosen, tenant.Status.AssignedNodes) {
		copy := tenant.DeepCopy()
		copy.Status.AssignedNodes = chosen
		if _, err := t.Base.Seterav1Clientset.SeteraV1().
			Tenants(namespace).
			UpdateStatus(ctx, copy, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating assignedNodes: %w", err)
		}
		t.Base.Recorder.Event(tenant, corev1.EventTypeNormal, "Assigned",
			fmt.Sprintf("selected nodes: %v", chosen))
	}

	return nil
}*/

// patchPauseStatus updates status.Paused and WaitingForNodeConfiguration
func (t *TenantOperator) patchPauseStatus(ctx context.Context, tenant *seterav1.Tenant, paused bool, waiting []string) error {
	copy := tenant.DeepCopy()
	copy.Status.Paused = paused
	copy.Status.Paused = paused
	copy.Status.WaitingForNodeConfiguration = waiting

	if _, err := t.Base.Seterav1Clientset.SeteraV1().
		Tenants(tenant.Namespace).
		UpdateStatus(ctx, copy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("patching pause status: %w", err)
	}

	evtType := "Resumed"
	msg := "All node scores present"
	if paused {
		evtType = "Paused"
		msg = fmt.Sprintf("waiting for scores from nodes: %v", waiting)
	}
	t.Base.Recorder.Event(tenant, corev1.EventTypeNormal, evtType, msg)

	// return error to requeue if paused
	if paused {
		return fmt.Errorf("tenant paused, requeueing")
	}
	return nil
}

// helper funcs

func containsString(slice []string, s string) bool {
	for _, e := range slice {
		if e == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	for i, e := range slice {
		if e == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
