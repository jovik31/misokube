package tenantcontroller

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	seterav1 "github/setera/pkg/api/setera.com/v1"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const assignedCondition = "Assigned"

func (c *Controller) updateTenantStatus(
	ctx context.Context,
	tenant *seterav1.Tenant,
	assignment assignmentResult,
) error {
	mod := tenant.DeepCopy()
	mod.Status.AssignedNodes = assignedNodeNames(assignment.nodes)

	status := metav1.ConditionFalse
	message := fmt.Sprintf(
		"tenant is assigned to %d of %d requested nodes",
		len(assignment.nodes),
		tenant.Spec.Zones,
	)

	if assignment.ready {
		status = metav1.ConditionTrue
		message = fmt.Sprintf(
			"tenant is assigned to %d nodes",
			len(assignment.nodes),
		)
	}

	apimeta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{
		Type:               assignedCondition,
		Status:             status,
		ObservedGeneration: tenant.Generation,
		Reason:             assignment.reason,
		Message:            message,
	})

	if reflect.DeepEqual(tenant.Status, mod.Status) {
		return nil
	}

	if _, err := c.setera.
		SeteraV1().
		Tenants().
		UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update tenant %s status: %w", tenant.Name, err)
	}

	return nil
}

func assignedNodeNames(nodes []*corev1.Node) []string {
	names := make([]string, 0, len(nodes))

	for _, node := range nodes {
		if node == nil {
			continue
		}
		names = append(names, node.Name)
	}

	sort.Strings(names)
	return names
}
