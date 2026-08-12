package webhook

import (
	"context"
	"encoding/json"
	"fmt"

	seterav1 "github/setera/pkg/api/setera.com/v1"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (ws *WebhookServer) validateTenant(
	admissionRequest *admissionv1.AdmissionRequest,
) (*admissionv1.AdmissionResponse, error) {
	var tenant seterav1.Tenant

	if err := json.Unmarshal(
		admissionRequest.Object.Raw,
		&tenant,
	); err != nil {
		return nil, fmt.Errorf(
			"decode Tenant: %w",
			err,
		)
	}

	nodes, err := ws.kubernetsClientset.
		CoreV1().
		Nodes().
		List(
			context.TODO(),
			metav1.ListOptions{},
		)
	if err != nil {
		return nil, fmt.Errorf(
			"list cluster nodes: %w",
			err,
		)
	}

	allowed, reason := checkNodeZones(
		nodes.Items,
		tenant.Status,
	)

	return createAdmissionResponse(
		allowed,
		reason,
	), nil
}
