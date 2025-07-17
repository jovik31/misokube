package pkg

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

// network configs
const (
	// DefaultMTU is the default MTU for network interfaces
	DefaultMTU          = 1500
	BrPrefix            = "br-"
	VxlanPrefix         = "vxlan-"
	MaxDeviceNameLength = 15 // max length for a network device name in Linux

)
