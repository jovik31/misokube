package webhook

import (
	misokubev1 "github/misokube/pkg/api/misokube.com/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	validateEndpoint = "/validate"
	serverPort       = ":8443"

	WebhookServiceName             = "misokube-webhook"
	WebhookConfigurationName       = "misokube-pod-admission"
	WebhookServicePort       int32 = 443

	contentTypeHeader = "content-type"
	contentTypeJSON   = "application/json"

	tenantNotFound             = "tenant not found"
	tenantNotAssigned          = "tenant is not assigned to nodes"
	tenantNodeSelectorConflict = "Pod has a conflicting MIsoKube tenant node selector"
	podNodeNameNotAllowed      = "tenant Pod must not set spec.nodeName"
	podIsValid                 = "Pod is valid"
	tenantIsValid              = "tenant is valid"
	zonesAboveNodes            = "the number of tenant zones is greater than the number of nodes"
)

var (
	deploymentGVK = metav1.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}

	daemonsetGVK = metav1.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "DaemonSet",
	}

	podGVK = metav1.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}

	tenantGVK = metav1.GroupVersionKind{
		Group:   misokubev1.SchemeGroupVersion.Group,
		Version: misokubev1.SchemeGroupVersion.Version,
		Kind:    misokubev1.SchemeGroupVersion.WithKind("Tenant").Kind,
	}
)
