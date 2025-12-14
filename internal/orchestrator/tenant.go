package orchestrator

import (

	//ard
	"context"
	"encoding/json"
	"fmt"
	"slices"

	// k8s
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	//setera
	seterav1 "github/setera/pkg/api/setera.com/v1"
)

// ensureTenantFinalizer patches metadata.finalizers if missing (idempotent).
func (o *Operator) ensureTenantFinalizer(ctx context.Context, t *seterav1.Tenant) error {
	if slices.Contains(t.Finalizers, tenantFinalizer) {
		return nil
	}
	finalizers := append(append([]string{}, t.Finalizers...), tenantFinalizer)

	payload := map[string]any{
		"metadata": map[string]any{
			"finalizers": finalizers,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal finalizer patch for %s/%s: %w", t.Namespace, t.Name, err)
	}

	if _, err := o.setera.SeteraV1().Tenants(t.Namespace).Patch(ctx, t.Name, types.MergePatchType, b, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch finalizers for %s/%s: %w", t.Namespace, t.Name, err)
	}
	return nil
}

// selectInitialAwaitingNodes returns up to t.Spec.Zones node IDs from current NodeStores.
// Non-deterministic selection is fine by your design.
func (o *Operator) selectInitialAwaitingNodes(t *seterav1.Tenant) ([]string, error) {
	if t.Spec.Zones <= 0 {
		return nil, nil
	}
	stores, err := o.nodeStoreLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list NodeStores: %w", err)
	}
	if len(stores) == 0 {
		o.logger.Info("no NodeStores available to select initial Tenant nodes", "tenant", fmt.Sprintf("%s/%s", t.Namespace, t.Name))
		return nil, fmt.Errorf("no NodeStores available")
	}

	out := make([]string, 0, t.Spec.Zones)
	for _, ns := range stores {
		if len(out) >= t.Spec.Zones {
			break
		}
		id := ns.Spec.Name
		if id == "" {
			id = ns.Name // fallback to object name if spec name not set
		}
		out = append(out, id)
	}

	return out, nil
}

// recomputeAwaitingAndAssignedForZones enforces the invariant:
// len(assigned)+len(awaiting) == zones, by:
// - adding to awaiting from available NodeStores when we need more
// - removing from assigned when we need fewer
// It also de-dups awaiting vs assigned (drops awaiting entries that are already assigned).
// Selection/removal order is non-deterministic by design.
func (o *Operator) recomputeAwaitingAndAssignedForZones(t *seterav1.Tenant, stores []*seterav1.NodeStore) ([]string, []seterav1.NodeInfo, bool) {

	// ensure non-negative zones
	zones := max(t.Spec.Zones, 0)

	assigned := t.Status.AssignedNodes
	awaitingOrig := t.Status.AwaitingNodeConfiguration
	assignedOrig := assigned

	// Build set of assigned IDs
	assignedIDs := make(map[string]bool, len(assigned))
	for _, ni := range assigned {
		if ni.Name != "" {
			assignedIDs[ni.Name] = true
		}
	}

	// Awaiting = awaitingOrig minus entries that are already assigned
	awaiting := make([]string, 0, len(awaitingOrig))
	for _, id := range awaitingOrig {
		if _, isAssigned := assignedIDs[id]; !isAssigned {
			awaiting = append(awaiting, id)
		}
	}

	// Compute how many more/less we need
	delta := zones - (len(assigned) + len(awaiting))

	switch {
	case delta > 0:
		// Need more: add to awaiting from stores not already used
		used := make(map[string]bool, len(assigned)+len(awaiting))
		for k := range assignedIDs {
			used[k] = true
		}
		for _, id := range awaiting {
			used[id] = true
		}
		// Prefer deterministic selection order by node identity
		// Build a stable list of candidate IDs from provided NodeStores
		candidateIDs := make([]string, 0, len(stores))
		for _, ns := range stores {
			if delta == 0 {
				break
			}
			id := ns.Spec.Name
			if id == "" {
				id = ns.Name
			}
			if id == "" {
				continue
			}
			candidateIDs = append(candidateIDs, id)
		}
		// Sort candidates for stable behavior
		slices.Sort(candidateIDs)
		for _, id := range candidateIDs {
			if delta == 0 {
				break
			}
			if _, seen := used[id]; seen {
				continue
			}
			awaiting = append(awaiting, id)
			used[id] = true
			delta--
		}
		// Best-effort if not enough nodes available.

	case delta < 0:
		// Need fewer: remove from assigned only
		remove := -delta
		if remove >= len(assigned) {
			assigned = assigned[:0]
		} else if remove > 0 {
			assigned = assigned[:len(assigned)-remove] // trim from end; order not important
		}
	}

	// Idempotency: only signal changed if something actually changed
	changed := !slices.Equal(awaitingOrig, awaiting) || !equalNodeInfos(assignedOrig, assigned)
	return awaiting, assigned, changed
}

// equalNodeInfos compares NodeInfo slices by value, ignoring order by keying on Name.
func equalNodeInfos(a, b []seterav1.NodeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]seterav1.NodeInfo, len(a))
	for _, x := range a {
		m[x.Name] = x
	}
	for _, y := range b {
		if x, ok := m[y.Name]; !ok || x != y {
			return false
		}
	}
	return true
}
