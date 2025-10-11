package ipam

import "net"

// NewIPAM constructs the default IPAM implementation for a given subnet.
// Currently returns a BitmapIPAM.
func NewIPAM(subnet *net.IPNet) (IPAM, error) {
    return NewBitmapIPAM(subnet)
}
