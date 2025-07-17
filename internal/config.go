package internal

// nodestore configs
const (
	NodeStoreFinalizer = "finalizer.setera.com/nodestore"
)

// tenant configs
const (
	TenantFinalizer = "finalizer.setera.com/tenant"
	PausedTenant    = true
	UnpausedTenant  = false
)

const (
	// setera_namespace is the namespace where Setera resources are deployed
	SeteraNamespace = "default"
)
