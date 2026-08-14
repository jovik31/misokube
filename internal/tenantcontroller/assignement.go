package tenantcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

type assignmentResult struct {
	nodes  []*corev1.Node
	reason string
	ready  bool
}

func (c *Controller) reconcileAssignments(
	ctx context.Context,
	tenant string,
	zones int,
) (assignmentResult, error) {
	labelKey, err := tenantmeta.NodeTenantLabel(tenant)
	if err != nil {
		return assignmentResult{}, err
	}

	nodes, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		return assignmentResult{}, fmt.Errorf(
			"list nodes: %w",
			err,
		)
	}

	assigned := assignedNodes(nodes, tenant)
	desired := max(zones, 0)

	if len(assigned) < desired {
		need := desired - len(assigned)

		candidates := scaleUpCandidates(
			nodes,
			tenant,
		)

		if need > len(candidates) {
			need = len(candidates)
		}

		for _, node := range candidates[:need] {
			if err := c.patchNodeTenantLabel(
				ctx,
				node.Name,
				labelKey,
				true,
			); err != nil {
				return assignmentResult{}, err
			}

			c.logger.Info(
				"assigned tenant to node",
				"tenant",
				tenant,
				"node",
				node.Name,
			)

			assigned = append(
				assigned,
				node,
			)
		}
	}

	if len(assigned) > desired {
		activePodNodes, err :=
			c.pods.ActiveTenantPodNodes(
				ctx,
				tenant,
			)
		if err != nil {
			return assignmentResult{}, err
		}

		removeCount :=
			len(assigned) - desired

		removable := scaleDownCandidates(
			assigned,
			activePodNodes,
		)

		if removeCount > len(removable) {
			removeCount = len(removable)
		}

		remove := make(
			map[string]struct{},
			removeCount,
		)

		for _, node := range removable[:removeCount] {
			if err := c.patchNodeTenantLabel(
				ctx,
				node.Name,
				labelKey,
				false,
			); err != nil {
				return assignmentResult{}, err
			}

			c.logger.Info(
				"removed tenant from node",
				"tenant",
				tenant,
				"node",
				node.Name,
			)

			remove[node.Name] = struct{}{}
		}

		if len(remove) != 0 {
			remaining := make(
				[]*corev1.Node,
				0,
				len(assigned)-len(remove),
			)

			for _, node := range assigned {
				if _, removed :=
					remove[node.Name]; removed {
					continue
				}

				remaining = append(
					remaining,
					node,
				)
			}

			assigned = remaining
		}
	}

	sort.Slice(
		assigned,
		func(i, j int) bool {
			return assigned[i].Name <
				assigned[j].Name
		},
	)

	switch {
	case len(assigned) < desired:
		return assignmentResult{
			nodes:  assigned,
			reason: "InsufficientNodes",
			ready:  false,
		}, nil

	case len(assigned) > desired:
		return assignmentResult{
			nodes:  assigned,
			reason: "ScaleDownBlocked",
			ready:  false,
		}, nil

	default:
		return assignmentResult{
			nodes:  assigned,
			reason: "Assigned",
			ready:  true,
		}, nil
	}
}

func assignedNodes(
	nodes []*corev1.Node,
	tenant string,
) []*corev1.Node {
	out := make([]*corev1.Node, 0)

	for _, node := range nodes {
		if tenantmeta.HasTenant(
			node.Labels,
			tenant,
		) {
			out = append(
				out,
				node,
			)
		}
	}

	return out
}

func scaleUpCandidates(
	nodes []*corev1.Node,
	tenant string,
) []*corev1.Node {
	out := make([]*corev1.Node, 0)

	for _, node := range nodes {
		if tenantmeta.HasTenant(
			node.Labels,
			tenant,
		) {
			continue
		}

		if !nodeEligible(node) {
			continue
		}

		out = append(
			out,
			node,
		)
	}

	sort.Slice(
		out,
		func(i, j int) bool {
			left :=
				tenantmeta.TenantCount(
					out[i].Labels,
				)

			right :=
				tenantmeta.TenantCount(
					out[j].Labels,
				)

			if left != right {
				return left < right
			}

			return out[i].Name <
				out[j].Name
		},
	)

	return out
}

func scaleDownCandidates(
	nodes []*corev1.Node,
	activePodNodes map[string]struct{},
) []*corev1.Node {
	out := make(
		[]*corev1.Node,
		0,
		len(nodes),
	)

	for _, node := range nodes {
		if _, hasPods :=
			activePodNodes[node.Name]; hasPods {
			continue
		}

		out = append(
			out,
			node,
		)
	}

	// Remove nodes with the most tenants first.
	// Use the node name to keep the result stable.
	sort.Slice(
		out,
		func(i, j int) bool {
			left :=
				tenantmeta.TenantCount(
					out[i].Labels,
				)

			right :=
				tenantmeta.TenantCount(
					out[j].Labels,
				)

			if left != right {
				return left > right
			}

			return out[i].Name >
				out[j].Name
		},
	)

	return out
}

func (c *Controller) patchNodeTenantLabel(
	ctx context.Context,
	nodeName string,
	labelKey string,
	present bool,
) error {
	var value any = "true"

	if !present {
		value = nil
	}

	payload := map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				labelKey: value,
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf(
			"marshal node label patch: %w",
			err,
		)
	}

	if _, err := c.kube.
		CoreV1().
		Nodes().
		Patch(
			ctx,
			nodeName,
			types.MergePatchType,
			data,
			metav1.PatchOptions{},
		); err != nil {
		return fmt.Errorf(
			"patch tenant label on node %s: %w",
			nodeName,
			err,
		)
	}

	return nil
}

func nodeEligible(
	node *corev1.Node,
) bool {
	if node == nil {
		return false
	}

	if node.Spec.Unschedulable {
		return false
	}

	if !nodeReady(node) {
		return false
	}

	if node.Labels[tenantmeta.NodeVTEPReadyLabel] != "true" {
		return false
	}

	if hasBlockingTaint(node) {
		return false
	}

	return true
}

func hasBlockingTaint(
	node *corev1.Node,
) bool {
	if node == nil {
		return false
	}

	for _, taint := range node.Spec.Taints {
		switch taint.Effect {
		case corev1.TaintEffectNoSchedule,
			corev1.TaintEffectNoExecute:
			return true
		}
	}

	return false
}

func nodeReady(
	node *corev1.Node,
) bool {
	if node == nil {
		return false
	}

	for _, condition := range node.Status.Conditions {
		if condition.Type !=
			corev1.NodeReady {
			continue
		}

		return condition.Status ==
			corev1.ConditionTrue
	}

	return false
}
