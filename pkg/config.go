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

// generic configs
const (
	// setera_namespace is the namespace where Setera resources are deployed
	SeteraNamespace = "default"
)

// network configs
const (

	// default MTU for the bridge interfaces
	DefaultMTU = 1500

	// device prefixes
	BrPrefix    = "br-"
	VxlanPrefix = "vxlan-"

	// max device name length
	MaxDeviceNameLength = 15 // max length for a network device name in Linux

	//vxlan confgis
	MaxVNI        = 16777215 // max VNI for VXLAN (24 bits, 2^24 - 1 = 16777215 = 0xFFFFFF
	VxlanPort     = 8472
	EncapOverhead = 50 // VXLAN encapsulation overhead (UDP + VXLAN + IP + Ethernet headers)

	//node cidr mask
	Node_CIDR_MASK = "/16"
)
