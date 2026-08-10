package webhook

import (
	seterav1 "github/setera/pkg/api/setera.com/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	validateEndpoint string = "/validate"
	//validatePodEndpoint        string = "/validate/pod"
	//validateTenantEndpoint     string = "/validate/tenant"
	//validateDeploymentEndpoint string = "validate/deployment"
	//validateDaemonsetEndpoint  string = "validate/daemonset" // not sure if i should validate daemonsets at this moment in time
	//tlsCert string = "tls.crt"
	//tlsKey  string = "tls.key"
	//tlsDir      string = "/run/secrets/tls"
	serverPort string = ":8443"

	contentTypeHeader string = "content-type"
	contentTypeJSON   string = "application/json"

	tenantNotFound      string = "tenant not found"
	tenantLabelNotFound string = "tenant label not found"
	podIsValid          string = "pod is valid"
	tenantIsValid       string = "tenant is valid"
	zonesAboveNodes     string = "the number of tenant zones is greater than the number of nodes"
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
		Kind:    "Daemonset",
	}

	podGVK = metav1.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}

	tenantGVK = metav1.GroupVersionKind{
		Group:   seterav1.SchemeGroupVersion.Group,
		Version: seterav1.SchemeGroupVersion.Version,
		Kind:    seterav1.SchemeGroupVersion.WithKind("Tenant").Kind,
	}
)
