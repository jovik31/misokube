// +kubebuilder:object:generate=true
// +groupName=setera.com
// +versionName=v1

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=tn
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// Tenant is a specification for a Tenant resource
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec"`
	Status TenantStatus `json:"status,omitempty"`
}

type TenantSpec struct {

	// +kubebuilder:validation:Minimum=1
	Zones int `json:"zones"` //Number of nodes where the tenant is to be deployed
}

type TenantStatus struct {
	// Conditions is a list of conditions for the tenant~

	// assigned nodes
	AssignedNodes []string `json:"assignedNodes,omitempty"` // list of nodes where the tenant is assigned

	// Conditions is a list of conditions for the tenant
	Conditions []metav1.Condition `json:"conditions,omitempty"` // list of conditions for the tenant
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// TenantList is a list of Tenant resources
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Tenant `json:"items,omitempty"`
}
