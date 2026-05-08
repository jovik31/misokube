package ipam

import (
	"fmt"
	"math/bits"
	"net"
	"sync"
)

var _ IPAM = (*BitmapIPAM)(nil) // ensure BitmapIPAM implements IPAM interface

// tenant subnet record
// SubnetRecord stores information about a tenant's subnet
type BitmapIPAM struct {
	mu      sync.Mutex
	Network *net.IPNet                   // the allocated subnet for this tenant
	bitmap  []uint64                     // bitmap for allocated IPs in the subnet
	IPs     map[string]*ContainerNetInfo // IPs : pod name --> ContainerNetInfo
}

// Stores container network information
type ContainerNetInfo struct {
	ID           string // container ID
	IFname       string //interface name
	NetNS        string // netns path and name
	IP           net.IP // allocated IP address for the container
	Ifindex      int    // host-side veth ifindex for this pod
	HostVethName string // actual host-side veth interface name (e.g., veth1a2b3c4d)
}

// newSubnetRecord initializes the bitmap, reserving ips for the bridge and vtep while excluding the broadcast address
func NewBitmapIPAM(subnet *net.IPNet) (IPAM, error) {
	if subnet == nil {
		return nil, ErrNilSubnet{}
	}

	ones, bits := subnet.Mask.Size()
	if bits != 32 {
		return nil, ErrIPv4OnlySupported{MaskSize: bits}
		// IPv6 support can be added later; for now assume IPv4.
	}

	total := 1 << (bits - ones) // includes network & broadcast
	if total < 4 {              // need more than 4 addresses to be useful
		return nil, ErrSubnetTooSmall{Subnet: subnet.String()}
	}

	// usable excludes network + broadcast
	usable := total - 2  // exclude network + broadcast
	podIPs := usable - 1 // exclude bridge (network+1)
	if podIPs <= 0 {
		return nil, ErrSubnetTooSmall{Subnet: subnet.String()}
	}
	// calculate number of 64-bit words needed to store podIPs
	words := (podIPs + 63) / 64
	bm := make([]uint64, words)

	return &BitmapIPAM{
		Network: subnet,
		bitmap:  bm,
		IPs:     make(map[string]*ContainerNetInfo),
	}, nil
}

// capacity returns total ip slots
func (ipam *BitmapIPAM) Capacity() (int, error) {
	ipam.mu.Lock()
	defer ipam.mu.Unlock()
	return ipam.capacityLocked()
}

func (ipam *BitmapIPAM) capacityLocked() (int, error) {
	if ipam.Network == nil {
		return 0, ErrNilSubnet{}
	}

	ones, bits := ipam.Network.Mask.Size()
	total := 1 << (bits - ones)
	usable := total - 2 // exclude network + broadcast
	if usable <= 1 {
		return 0, nil // nothing left after reserving bridge
	}
	return usable - 1, nil // exclude bridge (network+1)
}

// remaining returns free ip slots
func (ipam *BitmapIPAM) Remaining() (int, error) {
	ipam.mu.Lock()
	defer ipam.mu.Unlock()
	return ipam.remainingLocked()
}

func (ipam *BitmapIPAM) remainingLocked() (int, error) {
	cap, err := ipam.capacityLocked()
	if err != nil {
		return 0, nil // if capacity fails, return 0 remaining
	}
	used := 0
	for _, w := range ipam.bitmap {
		used += bits.OnesCount64(w) // count used bits in each 64-bit word
	}
	if used > cap {
		used = cap // sanity check, should not happen
	}

	return cap - used, nil

}

// assigns the first free ip in the bitmap to the pod and container information
func (ipam *BitmapIPAM) Allocate(podKey string, containerID string, ifName string, netns string) (*ContainerNetInfo, error) {

	ipam.mu.Lock()
	defer ipam.mu.Unlock()

	capacity, err := ipam.capacityLocked()
	if err != nil {
		return nil, err
	}
	if capacity <= 0 {
		return nil, ErrNoAvailableIPs{subnet: ipam.Network.String()}
	}
	// find first zero bit in the bitmap
	for idx, word := range ipam.bitmap {

		if ^word == 0 {
			continue // all bits are set, no free IPs in this block
		}
		free := ^word
		bit := bits.TrailingZeros64(free) // find first zero bit
		if bit >= 64 {
			continue
		}

		pos := idx*64 + bit // calculate position in the bitmap
		if pos >= capacity {
			break // out of bounds, should not happen
		}

		ipam.bitmap[idx] = word | (1 << bit) // set the bit
		ip := ipam.indexToIP(pos)            // convert position to IP

		res := &ContainerNetInfo{
			ID:      containerID,
			IFname:  ifName,
			NetNS:   netns,
			IP:      ip,
			Ifindex: -1,
		}
		ipam.IPs[podKey] = res

		return res, nil

	}

	return nil, ErrNoAvailableIPs{subnet: ipam.Network.String()}
}

// Free releases the given pod IP.
func (ipam *BitmapIPAM) Free(ip net.IP) error {
	ipam.mu.Lock()
	defer ipam.mu.Unlock()

	idx := ipam.ipToIndex(ip)
	if idx < 0 {
		return ErrIPNotInRange{IP: ip, Subn: ipam.Network}
	}
	wi, off := idx/64, uint(idx%64)
	if wi >= len(ipam.bitmap) {
		return fmt.Errorf("bitmap index out of range")
	}
	ipam.bitmap[wi] &^= (1 << off)

	for k, v := range ipam.IPs {
		if v.IP.Equal(ip) {
			delete(ipam.IPs, k)
			break
		}
	}
	return nil
}

func (ipam *BitmapIPAM) Expand(newSubnet *net.IPNet) error {
	ipam.mu.Lock()
	defer ipam.mu.Unlock()

	if newSubnet == nil {
		return ErrNilSubnet{}
	}
	if ipam.Network != nil && !cidrContains(newSubnet, ipam.Network) {
		return fmt.Errorf("ipam: new subnet %s does not include previous %s", newSubnet.String(), ipam.Network.String())
	}

	oldNet := ipam.Network
	ipam.Network = newSubnet

	capacity, err := ipam.capacityLocked()
	if err != nil {
		ipam.Network = oldNet
		return err
	}
	words := 0
	if capacity > 0 {
		words = (capacity + 63) / 64
	}
	newBitmap := make([]uint64, words)

	for podName, info := range ipam.IPs {
		if info == nil || info.IP == nil {
			delete(ipam.IPs, podName)
			continue
		}
		idx := ipam.ipToIndex(info.IP)
		if idx < 0 {
			ipam.Network = oldNet
			return fmt.Errorf("ipam: existing IP %s not in expanded subnet %s", info.IP.String(), newSubnet.String())
		}
		wi, off := idx/64, uint(idx%64)
		if wi >= len(newBitmap) {
			ipam.Network = oldNet
			return fmt.Errorf("ipam: bitmap index overflow for IP %s", info.IP.String())
		}
		newBitmap[wi] |= 1 << off
	}

	ipam.bitmap = newBitmap
	return nil
}

func (ipam *BitmapIPAM) ListAllocations() map[string]*ContainerNetInfo {
	ipam.mu.Lock()
	defer ipam.mu.Unlock()
	snap := make(map[string]*ContainerNetInfo, len(ipam.IPs))
	for k, v := range ipam.IPs {
		if v == nil {
			continue
		}
		copyInfo := *v
		snap[k] = &copyInfo
	}
	return snap
}

func (ipam *BitmapIPAM) GetAllocation(podName string) (*ContainerNetInfo, bool) {
	ipam.mu.Lock()
	defer ipam.mu.Unlock()
	info, ok := ipam.IPs[podName]
	if !ok || info == nil {
		return nil, false
	}
	copyInfo := *info
	return &copyInfo, true
}

// SetHostVethName stores the host-side veth interface name for the given pod allocation.
func (ipam *BitmapIPAM) SetHostVethName(podName string, hostIf string) error {
	ipam.mu.Lock()
	defer ipam.mu.Unlock()
	info, ok := ipam.IPs[podName]
	if !ok || info == nil {
		return fmt.Errorf("no allocation for pod %s", podName)
	}
	info.HostVethName = hostIf
	return nil
}

// indexToIP maps bitmap index (0 = network+2) -> IP.
func (ipam *BitmapIPAM) indexToIP(idx int) net.IP {
	base := ipam.Network.IP.Mask(ipam.Network.Mask).To4()
	return addIPv4(base, 2+idx)
}

// ipToIndex converts IP to bitmap index or -1 if outside pod range.
func (ipam *BitmapIPAM) ipToIndex(ip net.IP) int {
	if ipam.Network == nil {
		return -1
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return -1
	}
	base := ipam.Network.IP.Mask(ipam.Network.Mask).To4()
	offset := ipv4Diff(base, ip4)
	ones, bitsz := ipam.Network.Mask.Size()
	total := 1 << (bitsz - ones)

	// Pod IP offsets: network+2 .. broadcast-1
	if offset < 2 || offset >= total-1 {
		return -1
	}
	return offset - 2
}

func cidrContains(outer, inner *net.IPNet) bool {
	if outer == nil || inner == nil {
		return false
	}
	return outer.Contains(inner.IP) && outer.Contains(lastIP(inner))
}

func lastIP(subnet *net.IPNet) net.IP {
	if subnet == nil {
		return nil
	}
	ip := subnet.IP.To4()
	if ip == nil {
		ip = subnet.IP.To16()
	}
	if ip == nil {
		return nil
	}
	end := make(net.IP, len(ip))
	copy(end, ip)
	for i := range end {
		end[i] |= ^subnet.Mask[i]
	}
	return end
}
