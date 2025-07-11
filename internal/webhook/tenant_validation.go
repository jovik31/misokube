package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	seterav1 "github/setera/pkg/api/setera.com/v1"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

func (ws *WebhookServer) validateTenant(admissionRequest *admissionv1.AdmissionRequest) (*admissionv1.AdmissionResponse, error) {

	var tenant seterav1.Tenant

	if err := json.Unmarshal(admissionRequest.Object.Raw, &tenant); err != nil {
		klog.Info(fmt.Printf("Error in unmarshalling tenant: %v", err))
		return nil, err
	}

	var nodes *corev1.NodeList
	var err error

	if nodes, err = ws.kubernetsClientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{}); err != nil {
		klog.Info(fmt.Printf("Error in fetching cluster node list: %v", err))
		return nil, err
	}

	allowed, reason := checkNodeZones(nodes.Items, tenant.Status)

	return createAdmissionResponse(allowed, reason), nil

	/*

		- check number of nodes against number of zones
		- check tenantSelectors against node selectors
		- reject:
			- number of zones is larger than the number of cluster nodes
			- zone selectors are not complied by node selectors
	*/

}
