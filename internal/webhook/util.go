package webhook

import (
	seterav1 "github/setera/pkg/api/setera.com/v1"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func isMapped[K, V comparable](target V, m map[K]V) bool {

	for _, v := range m {
		if v == target {
			return true
		}
	}
	return false
}

func isMapSubset[K, V comparable](m, sub map[K]V) bool {

	// if zone requirements are larger than node labels
	if len(sub) > len(m) {
		return false
	}

	for k, vsub := range sub {
		// if value not found or different between nodes and zones
		if vm, found := m[k]; !found || vm != vsub {
			return false
		}
	}
	return true
}

// check tenant should be a universal helper not bound to a k8s kind or resource
func checkTenantLabel(labels map[string]string, tenantList *seterav1.TenantList) (bool, string) {

	var allowed bool = true
	// check if tenant label is present
	tenant_label, ok := labels[tenantLabelKey]
	if !ok {
		return !allowed, tenantLabelNotFound
	}

	// check if tenant label is valid
	for _, tenant := range tenantList.Items {

		if tenant_label == tenant.Name {
			return allowed, tenantIsValid
		}
	}

	return !allowed, tenantNotFound
}

func checkNodeZones(nodeList []corev1.Node, status seterav1.TenantStatus) (bool, string) {

	zoneList := status.AssignedNodes

	var allowed bool = true

	//check number of nodes against number of zones
	if len(nodeList) < len(zoneList) {

		return !allowed, zonesAboveNodes
	}

	return allowed, tenantIsValid

}

// creates admission response
func createAdmissionResponse(result bool, msg string) *admissionv1.AdmissionResponse {

	admissionResponse := &admissionv1.AdmissionResponse{

		Allowed: result,
		Result: &metav1.Status{
			Message: msg,
		},
	}

	return admissionResponse
}

// create admission review response with info for the respective request
func newAdmissionReview(aReq admissionv1.AdmissionReview, aRes *admissionv1.AdmissionResponse) admissionv1.AdmissionReview {

	var responseAdmissionReview = admissionv1.AdmissionReview{}

	responseAdmissionReview.Response = aRes
	responseAdmissionReview.SetGroupVersionKind(aReq.GroupVersionKind())
	responseAdmissionReview.Response.UID = aReq.Request.UID

	return responseAdmissionReview

}
