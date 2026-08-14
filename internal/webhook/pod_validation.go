package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/tenantmeta"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const tenantAssignedCondition = "Assigned"

type jsonPatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

func (ws *WebhookServer) admitPod(
	ctx context.Context,
	admissionRequest *admissionv1.AdmissionRequest,
) (*admissionv1.AdmissionResponse, error) {
	var pod corev1.Pod

	if err := json.Unmarshal(
		admissionRequest.Object.Raw,
		&pod,
	); err != nil {
		return nil, fmt.Errorf(
			"decode Pod: %w",
			err,
		)
	}

	tenantName := tenantmeta.ResolvePodTenant(
		pod.Namespace,
		pod.Labels,
	)

	if tenantName == tenantmeta.DefaultTenant {
		return createAdmissionResponse(
			true,
			podIsValid,
		), nil
	}

	tenant, err := ws.seterav1Clientset.
		SeteraV1().
		Tenants().
		Get(
			ctx,
			tenantName,
			metav1.GetOptions{},
		)

	if apierrors.IsNotFound(err) {
		return createAdmissionResponse(
			false,
			tenantNotFound,
		), nil
	}

	if err != nil {
		return nil, fmt.Errorf(
			"get Tenant %q: %w",
			tenantName,
			err,
		)
	}

	if !tenantIsAssigned(tenant) {
		return createAdmissionResponse(
			false,
			tenantNotAssigned,
		), nil
	}

	if pod.Spec.NodeName != "" {
		return createAdmissionResponse(
			false,
			podNodeNameNotAllowed,
		), nil
	}

	tenantNodeLabel, err :=
		tenantmeta.NodeTenantLabel(tenantName)
	if err != nil {
		return createAdmissionResponse(
			false,
			err.Error(),
		), nil
	}

	for key, value := range pod.Spec.NodeSelector {
		if !strings.HasPrefix(
			key,
			tenantmeta.NodeTenantLabelPrefix,
		) {
			continue
		}

		if key != tenantNodeLabel ||
			value != "true" {
			return createAdmissionResponse(
				false,
				tenantNodeSelectorConflict,
			), nil
		}
	}

	if pod.Spec.NodeSelector[tenantNodeLabel] ==
		"true" {
		return createAdmissionResponse(
			true,
			podIsValid,
		), nil
	}

	nodeSelector := make(
		map[string]string,
		len(pod.Spec.NodeSelector)+1,
	)

	for key, value := range pod.Spec.NodeSelector {
		nodeSelector[key] = value
	}

	nodeSelector[tenantNodeLabel] = "true"

	patch, err := json.Marshal(
		[]jsonPatchOperation{
			{
				Op:    "add",
				Path:  "/spec/nodeSelector",
				Value: nodeSelector,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create Pod admission patch: %w",
			err,
		)
	}

	patchType := admissionv1.PatchTypeJSONPatch

	response := createAdmissionResponse(
		true,
		podIsValid,
	)

	response.PatchType = &patchType
	response.Patch = patch

	return response, nil
}

func tenantIsAssigned(
	tenant *seterav1.Tenant,
) bool {
	if tenant == nil {
		return false
	}

	condition := apimeta.FindStatusCondition(
		tenant.Status.Conditions,
		tenantAssignedCondition,
	)

	if condition == nil {
		return false
	}

	return condition.Status ==
		metav1.ConditionTrue &&
		condition.ObservedGeneration ==
			tenant.Generation
}
