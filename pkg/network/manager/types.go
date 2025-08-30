package manager

import "net"

type Event int

type PeerSpec struct {
	RemoteNodeIP     net.IP
	RemoteVtepIP     net.IP
	RemoteVtepMAC    net.HardwareAddr
	RemoteTenantCIDR *net.IPNet
	NodeName         string
}

type Request struct {
	Event    Event
	TenantID string
	Peer     *PeerSpec
	PodSpec  *PodSpec
}
