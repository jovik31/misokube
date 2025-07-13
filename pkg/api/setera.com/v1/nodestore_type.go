// +kubebuilder:object:generate=true
// +groupName=setera.com
// +versionName=v1

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// NodeStore is a specification for a NodeStore resource
type NodeStore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeStoreSpec   `json:"spec"`
	Status NodeStoreStatus `json:"status,omitempty"`
}

type NodeStoreSpec struct {
	Name   string `json:"name"`   // Node name
	NodeIP string `json:"nodeIP"` // Node IP address

	// +kubebuilder:validation:Optional
	Selectors map[string]string `json:"selectors"` // Node selectors obtained from node labels to compare against the tenant selectors provided
}

type NodeStoreStatus struct {

	// +kubebuilder:validation:Optional
	Tenants map[string]TenantInfra `json:"tenants"` // Tenants that are deployed on this node
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// TenantList is a list of Tenant resources
type NodeStoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []NodeStore `json:"items,omitempty"`
}

type TenantInfra struct {
	Name       string     `json:"name"`        //Tenant Name
	TenantCIDR string     `json:"tenant_cidr"` //Tenant CIDR
	VNI        int        `json:"vni"`         //Tenant VNI identification
	VTEP_IP    string     `json:"vtep_ip"`     //VTEP IP address
	VTEP_MAC   string     `json:"vtep_mac"`    //VTEP MAC address
	BRIDGE_IP  string     `json:"bridge_ip"`   //Bridge IP address
	BRIDGE_MAC string     `json:"bridge_mac"`  //Bridge MAC address
	Pods       []Pod_Info `json:"pods"`        //Pods that are deployed on this tenant

}

type Pod_Info struct {
	Name string `json:"name"` //Pod Name
	IP   string `json:"ip"`   //Pod IP address
}
