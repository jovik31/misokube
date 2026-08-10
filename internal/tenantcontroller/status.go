package tenantcontroller

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const assignedCondition = "Assigned"

func (c *Controller) updateTenantStatus(ctx context.Context, tenant *seterav1.Tenant, assignment assignmentResult) error {
	mod := tenant.DeepCopy()
	mod.Status.AssignedNodes = nodeInfos(assignment.nodes)

	// Node labels are now the assignment source. The old waiting list is kept
	// empty until the field is removed from the Tenant API.
	mod.Status.AwaitingNodeConfiguration = nil

	conditionStatus := metav1.ConditionFalse
	message := fmt.Sprintf("tenant is assigned to %d of %d requested nodes", len(assignment.nodes), tenant.Spec.Zones)
	if assignment.ready {
		conditionStatus = metav1.ConditionTrue
		message = fmt.Sprintf("tenant is assigned to %d nodes", len(assignment.nodes))
	}

	apimeta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{
		Type:               assignedCondition,
		Status:             conditionStatus,
		ObservedGeneration: tenant.Generation,
		Reason:             assignment.reason,
		Message:            message,
	})

	if reflect.DeepEqual(tenant.Status, mod.Status) {
		return nil
	}

	if _, err := c.setera.SeteraV1().Tenants(mod.Namespace).UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update tenant %s status: %w", tenant.Name, err)
	}
	return nil
}

func nodeInfos(nodes []*corev1.Node) []seterav1.NodeInfo {
	out := make([]seterav1.NodeInfo, 0, len(nodes))
	for _, node := range nodes {
		info := seterav1.NodeInfo{
			Name:   node.Name,
			NodeIP: nodeInternalIP(node),
		}

		if vtep, ready, err := tenantmeta.VTEPFromMetadata(node.Labels, node.Annotations); err == nil && ready {
			info.VtepIP = vtep.IP.String()
			info.VtepMAC = vtep.MAC.String()
		}

		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func nodeInternalIP(node *corev1.Node) string {
	if node == nil {
		return ""
	}
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address
		}
	}
	return ""
}
