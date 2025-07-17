package network

import (
	"fmt"
	"math/big"
	"net"
)

// tenant subnet record
// SubnetRecord stores information about a tenant's subnet
type SubnetRecord struct {
	Network *net.IPNet
	Bridge  *BridgeRecord
	VTEP    *VxlanRecord

	bitmap []int                       // bitmap for allocated IPs in the subnet
	IPs    map[string]ContainerNetInfo // IPs allocated to containers in this subnet
}

// Stores network device data
type BridgeRecord struct {
	Name      string
	GatewayIP string // Gateway IP for the bridge
}

type VxlanRecord struct {
	Name string
	IP   string
	MAC  string
	VNI  int
}

// Stores container network information
type ContainerNetInfo struct {
	ID     string // container ID
	IFname string //interface name
	NetNS  string // netns path and name
	Name   string // container name
	IP     string // allocated IP address
}

// newSubnetRecord initializes the bitmap, reserving reservedCount bits up front.
func newSubnetRecord(cidr *net.IPNet, reservedCount int) *SubnetRecord {
	ones, bits := cidr.Mask.Size()
	hostCount := 1<<(bits-ones) - 2 // total usable hosts
	// round up to a multiple of 64
	size := (hostCount + 63) / 64
	bm := make([]int, size)

	// mark the first `reservedCount` bits as used (e.g. network addr, bridge, vtep)
	for i := 0; i < reservedCount; i++ {
		idx, off := i/64, uint(i%64)
		bm[idx] |= 1 << off
	}

	return &SubnetRecord{
		Network: cidr,
		Bridge:  nil,
		VTEP:    nil,
		bitmap:  bm,
		IPs:     make(map[string]ContainerNetInfo),
	}
}

// AllocatePodIP finds the first zero bit in the bitmap, sets it, and returns that IP.
func (sr *SubnetRecord) AllocatePodIP(containerID, ifname, netns, name string) (ContainerNetInfo, error) {
	hostIP := func(pos int) net.IP {
		// same hostIP from before
		base := sr.Network.IP.Mask(sr.Network.Mask)
		ipInt := big.NewInt(0).SetBytes(base.To4())
		ipInt.Add(ipInt, big.NewInt(int64(pos+1))) // +1 to skip network address
		b := ipInt.Bytes()
		if len(b) < 4 {
			p := make([]byte, 4)
			copy(p[4-len(b):], b)
			b = p
		}
		return net.IP(b)
	}

	// scan bitmap for first zero bit
	for idx, word := range sr.bitmap {
		if ^word == 0 {
			continue // all ones, no free in this block
		}
		// there's at least one zero bit in this 64-bit word
		for off := uint(0); off < 64; off++ {
			if (word>>off)&1 == 0 {
				pos := idx*64 + int(off)
				ip := hostIP(pos)
				info := ContainerNetInfo{ID: containerID, IFname: ifname, NetNS: netns, Name: name, IP: ip.String()}
				sr.IPs[containerID] = info
				// mark bit used
				sr.bitmap[idx] |= 1 << off
				return info, nil
			}
		}
	}

	return ContainerNetInfo{}, fmt.Errorf("no free IPs in subnet %v", sr.Network)
}

// ReleasePodIP clears the bit for the released containerID
func (sr *SubnetRecord) ReleasePodIP(containerID string) {
	info, ok := sr.IPs[containerID]
	if !ok {
		return
	}
	// compute position from info.IP
	ip := net.ParseIP(info.IP).To4()
	// index = (ipInt - baseInt) - 1
	base := sr.Network.IP.Mask(sr.Network.Mask)
	baseInt := big.NewInt(0).SetBytes(base)
	ipInt := big.NewInt(0).SetBytes(ip)
	diff := big.NewInt(0).Sub(ipInt, baseInt).Int64() - 1
	if diff >= 0 {
		pos := int(diff)
		idx, off := pos/64, uint(pos%64)
		sr.bitmap[idx] &^= 1 << off
	}
	delete(sr.IPs, containerID)
}
