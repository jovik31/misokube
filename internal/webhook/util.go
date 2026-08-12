package webhook

import (
	seterav1 "github/setera/pkg/api/setera.com/v1"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func isMapped[K, V comparable](
	target V,
	values map[K]V,
) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func isMapSubset[K, V comparable](
	values map[K]V,
	subset map[K]V,
) bool {
	if len(subset) > len(values) {
		return false
	}

	for key, expected := range subset {
		value, found := values[key]

		if !found || value != expected {
			return false
		}
	}

	return true
}

func checkNodeZones(
	nodeList []corev1.Node,
	status seterav1.TenantStatus,
) (bool, string) {
	if len(nodeList) <
		len(status.AssignedNodes) {
		return false, zonesAboveNodes
	}

	return true, tenantIsValid
}

func createAdmissionResponse(
	allowed bool,
	message string,
) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: allowed,
		Result: &metav1.Status{
			Message: message,
		},
	}
}

func newAdmissionReview(
	requestReview admissionv1.AdmissionReview,
	response *admissionv1.AdmissionResponse,
) admissionv1.AdmissionReview {
	responseReview :=
		admissionv1.AdmissionReview{}

	responseReview.Response = response

	responseReview.SetGroupVersionKind(
		requestReview.GroupVersionKind(),
	)

	responseReview.Response.UID =
		requestReview.Request.UID

	return responseReview
}
