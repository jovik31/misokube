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

	scoreList := make([]struct {
		Name  string
		Score int
	}, len(stores))

	for i, ns := range stores {
		scoreList[i].Name = ns.Spec.Name
		subnetScore, err := o.getNodeScoreFreeSubnets(*ns)
		metricsScore, err2 := o.getNodeScoreMetrics(context.Background(), *ns, t.Name)

		if err != nil || err2 != nil {
			o.logger.Error(err, "failed to get node subnet score", "nodestore", fmt.Sprintf("%s/%s", ns.Namespace, ns.Name))
			o.logger.Error(err2, "failed to get node metrics score", "nodestore", fmt.Sprintf("%s/%s", ns.Namespace, ns.Name))
			continue
		}

		// Both scores are 0-100. Use whichever is available; average if both are.
		var nodeScore float64
		switch {
		case err != nil:
			// Only metrics available
			o.logger.Info("subnet score unavailable, using metrics only", "node", ns.Spec.Name, "err", err)
			nodeScore = metricsScore
		case err2 != nil:
			// Only subnet score available
			o.logger.Info("metrics score unavailable, using subnet score only", "node", ns.Spec.Name, "err", err2)
			nodeScore = subnetScore
		default:
			nodeScore = (subnetScore * 0.5) + (metricsScore * 0.5)
		}
		scoreList[i].Score = int(nodeScore)
	}

	// Sort by score in descending order
	slices.SortFunc(scoreList, func(a, b struct {
		Name  string
		Score int
	}) int {
		return b.Score - a.Score
	})

	o.logger.Info("scored NodeStores for initial Tenant node selection", "tenant", fmt.Sprintf("%s/%s", t.Namespace, t.Name), "scores", scoreList)

	out := make([]string, 0, t.Spec.Zones)
	//Gets the nodestores in a random order and then pulls them one by one until we have as many nodes as zones.
	for _, node := range scoreList {
		//Already have enough nodes
		if len(out) >= t.Spec.Zones {
			break
		}
		id := node.Name
		if id == "" {
			id = node.Name // use the name from scoreList
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

// getNodeScore retrieves a node's resource Allocatble capacity and computes a weighted score.
func (o *Operator) getNodeAllocatable(ctx context.Context, ns seterav1.NodeStore) (float64, float64, error) {
	// Get the node in the nodestore
	node, err := o.kubeclient.CoreV1().Nodes().Get(ctx, ns.Spec.Name, metav1.GetOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get node %s: %v", ns.Spec.Name, err)
	}

	// Get the CPU and memory capacity of the node
	cpu := node.Status.Allocatable.Cpu().MilliValue()
	mem := node.Status.Allocatable.Memory().Value()

	o.logger.Info("got node capacity for scoring", "node", node.Name, "cpu(millicores)", cpu, "memory(bytes)", mem)

	//Convert from cores to milicores on the CPU and from ki to Mi on memory
	return float64(cpu), float64(mem), nil
}

func (o *Operator) getNodeMetrics(ctx context.Context, ns seterav1.NodeStore, tenantName string) (float64, float64, error) {
	// Get the node in the nodestore
	m, err := o.metricsClient.MetricsV1beta1().NodeMetricses().Get(ctx, ns.Spec.Name, metav1.GetOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get node metrics for node %s on tenant %s: %v", ns.Spec.Name, tenantName, err)
	}

	cpu := m.Usage.Cpu().MilliValue()
	mem := m.Usage.Memory().Value()

	o.logger.Info("got node metrics for scoring", "node", ns.Spec.Name, "cpu(millicores)", cpu, "memory(bytes)", mem)

	return float64(cpu), float64(mem), nil
}

func (o *Operator) getNodeScoreMetrics(ctx context.Context, ns seterav1.NodeStore, tenantName string) (float64, error) {
	cpuUsage, memUsage, err := o.getNodeMetrics(ctx, ns, tenantName)
	if err != nil {
		return 0, fmt.Errorf("get node metrics: %w", err)
	}

	cpuMax, memMax, err := o.getNodeAllocatable(ctx, ns)
	if err != nil {
		return 0, fmt.Errorf("get node allocatable: %w", err)
	}

	if cpuMax == 0 || memMax == 0 {
		return 0, fmt.Errorf("node %s has zero allocatable CPU or memory", ns.Spec.Name)
	}

	cpuScore := cpuUsage / cpuMax
	memScore := memUsage / memMax

	// Simple average of CPU and memory usage ratios inverted to represent
	// free capacity: higher score = more resources available.
	score := (cpuScore + memScore) / 2

	o.logger.Info("computed node score from metrics", "node", ns.Spec.Name, "cpuUsage", cpuUsage, "memUsage", memUsage, "cpuMax", cpuMax, "memMax", memMax, "score", score)
	
	return (1-score)*100, nil
}


func (o *Operator) getNodeScoreFreeSubnets(ns seterav1.NodeStore) (float64, error) {
    if ns.Status.TotalSubnets == 0 {
        return 0, fmt.Errorf("total subnets is zero, cannot compute free subnet ratio")
    }
	o.logger.Info("got node subnet counts for scoring", "node", ns.Spec.Name, "freeSubnets", ns.Status.FreeSubnets, "totalSubnets", ns.Status.TotalSubnets)
    return (1-(float64(ns.Status.FreeSubnets)/float64(ns.Status.TotalSubnets)))*100, nil
}

