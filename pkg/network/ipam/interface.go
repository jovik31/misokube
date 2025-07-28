package ipam

import "net"

type IPAM interface {

	// allocate ip
	Allocate(containerID, ifName, netNs, podName string) (*ContainerNetInfo, error)

	// free ip
	Free(ip net.IP) error

	// get the number of IPs available in the subnet.
	Capacity() (int, error)

	// get the number of free IPs available in the subnet.
	Remaining() int

	// expand the subnet
	Expand()
}
