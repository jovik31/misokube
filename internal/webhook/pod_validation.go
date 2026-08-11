package webhook

import (
	"encoding/json"
	"fmt"

	"context"

	//k8s
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/klog/v2"
)

/* [ ] Implement pod mutation webhook
   [ ]: Check if pod has the tenant label
   [ ]: If not, add default tenant
   [ ] If it has a tenant validate the tenant
*/

func (ws *WebhookServer) validatePod(admissionRequest *admissionv1.AdmissionRequest) (*admissionv1.AdmissionResponse, error) {

	var pod corev1.Pod
	if err := json.Unmarshal(admissionRequest.Object.Raw, &pod); err != nil {
		klog.Info(fmt.Printf("Error in unmarshalling pod: %v", err))
		return nil, err
	}

	// Fetch tenant list, since tenants are cluster scoped we use "" as the namespace
	tenantList, err := ws.seterav1Clientset.SeteraV1().Tenants().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Info(fmt.Printf("Error in fetching tenant list: %v", err))

		return nil, err
	}

	allowed, reason := checkTenantLabel(pod.Labels, tenantList)

	return createAdmissionResponse(allowed, reason), nil

}
