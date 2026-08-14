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
	"k8s.io/client-go/util/retry"
)

const assignedCondition = "Assigned"

func (c *Controller) updateTenantStatus(
	ctx context.Context,
	tenant *seterav1.Tenant,
	assignment assignmentResult,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := c.setera.
			SeteraV1().
			Tenants().
			Get(ctx, tenant.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get current tenant %s: %w", tenant.Name, err)
		}

		// The desired state changed while this reconciliation was running.
		// Do not write status calculated from the old generation. The informer
		// will enqueue the new generation for a fresh reconciliation.
		if current.Generation != tenant.Generation {
			return nil
		}

		mod := current.DeepCopy()
		mod.Status.AssignedNodes = assignedNodeNames(assignment.nodes)

		status := metav1.ConditionFalse
		message := fmt.Sprintf(
			"tenant is assigned to %d of %d requested nodes",
			len(assignment.nodes),
			current.Spec.Zones,
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
			ObservedGeneration: current.Generation,
			Reason:             assignment.reason,
			Message:            message,
		})

		if reflect.DeepEqual(current.Status, mod.Status) {
			return nil
		}

		if _, err := c.setera.
			SeteraV1().
			Tenants().
			UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
			return err
		}

		return nil
	})
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
