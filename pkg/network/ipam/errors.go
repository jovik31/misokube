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
