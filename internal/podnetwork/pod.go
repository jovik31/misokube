package podnetwork

import "net/netip"

// Request identifies one local pod network attachment.
type Request struct {
	ContainerID string
	NetNS       string
	IfName      string

	PodUID   string
	TenantID string
}

// Result is the local network state returned after a successful ADD.
type Result struct {
	IP netip.Addr

	HostVethName    string
	HostVethIfIndex int
}

// LocalPod contains the local pod data required by the datapath.
type LocalPod struct {
	IP netip.Addr

	PodUID   string
	TenantID string

	HostVethName    string
	HostVethIfIndex int
}
