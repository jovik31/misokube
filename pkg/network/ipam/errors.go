package ipam

import (
	"fmt"
	"net"
)

type ErrNilSubnet struct{}

func (e ErrNilSubnet) Error() string {
	return "ipam: nil subnet provided"
}

type ErrNoAvailableIPs struct {
	subnet string
}

func (e ErrNoAvailableIPs) Error() string {
	return "ipam: no available IPs in the subnet"
}

type ErrIPNotInRange struct {
	IP   net.IP
	Subn *net.IPNet
}

func (e ErrIPNotInRange) Error() string {
	return fmt.Sprintf("ipam: IP %s is not in subnet %s", e.IP, e.Subn)
}

// call to free of an already not allocated ip
type ErrIPNotAllcated struct {
	IP net.IP
}

func (e ErrIPNotAllcated) Error() string {
	return fmt.Sprintf("ipam: IP %s is not allocated", e.IP)
}

// only ipv4 is supported
type ErrIPv4OnlySupported struct {
	MaskSize int
}

func (e ErrIPv4OnlySupported) Error() string {

	return fmt.Sprintf("only IPv4 supported (got mask bits=%d)", e.MaskSize)
}

// subnet too small for IPAM
type ErrSubnetTooSmall struct {
	Subnet string
}

func (e ErrSubnetTooSmall) Error() string {
	return fmt.Sprintf("ipam: subnet too small for IPAM (got %s)", e.Subnet)
}

// subnet larger than the node IPAM accepts, which points at a bad PodCIDR
type ErrSubnetTooLarge struct {
	Subnet string
	Max    int
}

func (e ErrSubnetTooLarge) Error() string {
	return fmt.Sprintf("ipam: subnet %s holds more than %d addresses", e.Subnet, e.Max)
}

// the node IPAM needs a store to keep its allocations across a restart
type ErrNilStore struct{}

func (e ErrNilStore) Error() string {
	return "ipam: nil store provided"
}

// a request or a call with no container ID, which is the allocation identity
type ErrEmptyContainerID struct{}

func (e ErrEmptyContainerID) Error() string {
	return "ipam: empty container ID"
}

// a request with no tenant, which the policy layer needs
type ErrEmptyTenantID struct {
	ContainerID string
}

func (e ErrEmptyTenantID) Error() string {
	return fmt.Sprintf("ipam: request for container %s has no tenant ID", e.ContainerID)
}

// a request with no pod name, which the secondary index needs
type ErrEmptyPodName struct {
	ContainerID string
}

func (e ErrEmptyPodName) Error() string {
	return fmt.Sprintf("ipam: request for container %s has no pod name", e.ContainerID)
}

// a call for a container that holds no allocation
type ErrAllocationNotFound struct {
	ContainerID string
}

func (e ErrAllocationNotFound) Error() string {
	return fmt.Sprintf("ipam: no allocation for container %s", e.ContainerID)
}

// a stored allocation that the node PodCIDR does not contain. The PodCIDR
// changed, or the state directory belongs to a different node.
type ErrStoredIPOutOfRange struct {
	ContainerID string
	IP          net.IP
	Subnet      *net.IPNet
}

func (e ErrStoredIPOutOfRange) Error() string {
	return fmt.Sprintf("ipam: stored IP %s for container %s is outside subnet %s", e.IP, e.ContainerID, e.Subnet)
}

// two stored allocations that name the same address
type ErrStoredIPConflict struct {
	IP           net.IP
	ContainerIDs [2]string
}

func (e ErrStoredIPConflict) Error() string {
	return fmt.Sprintf("ipam: stored IP %s is claimed by containers %s and %s", e.IP, e.ContainerIDs[0], e.ContainerIDs[1])
}
