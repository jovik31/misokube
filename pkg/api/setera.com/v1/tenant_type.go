// +kubebuilder:object:generate=true
// +groupName=setera.com
// +versionName=v1

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=tn
// Tenant is a specification for a Tenant resource
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec"`
	Status TenantStatus `json:"status,omitempty"`
}

type TenantSpec struct {
	Name  string `json:"name"`  //Tenant Name
	Zones int    `json:"zones"` //Number of nodes where the tenant is to be deployed
}

type TenantStatus struct {
	// Conditions is a list of conditions for the tenant~

	// assigned nodes
	AssignedNodes []NodeInfo `json:"assignedNodes,omitempty"` // list of nodes where the tenant is assigned

	// paused is true if the tenant is paused
	Paused bool `json:"paused,omitempty"` // tenant is paused

	AwaitingNodeConfiguration []string `json:"waitingForNodeConfiguration,omitempty"` // list of nodes where the tenant is waiting for configuration

	// Conditions is a list of conditions for the tenant
	Conditions []metav1.Condition `json:"conditions,omitempty"` // list of conditions for the tenant
}

type NodeInfo struct {
	Name    string `json:"name"`              // Name of the node
	IP      string `json:"ip"`                // IP address of the node
	VtepIP  string `json:"vtepIP,omitempty"`  // VTEP IP address of the node, if applicable
	VtepMAC string `json:"vtepMAC,omitempty"` // VTEP MAC address of the node, if applicable
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// TenantList is a list of Tenant resources
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Tenant `json:"items,omitempty"`
}
