package main

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=list
// +kubebuilder:rbac:groups=setera.com,resources=tenants,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=setera.com,resources=tenants/status,verbs=update
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;create;update
