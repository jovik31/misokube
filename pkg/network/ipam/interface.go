package ipam

import "net"

type IPAM interface {

	// allocate ip
	Allocate(podKey, containerID, ifName, netNs string) (*ContainerNetInfo, error)

	// free ip
	Free(ip net.IP) error

	// get the number of IPs available in the subnet.
	Capacity() (int, error)

	// get the number of free IPs available in the subnet.
	Remaining() (int, error)

	// expand the subnet to the provided CIDR
	Expand(newSubnet *net.IPNet) error
	ListAllocations() map[string]*ContainerNetInfo
	GetAllocation(podName string) (*ContainerNetInfo, bool)
	SetHostVethName(podName string, hostIf string) error
}
