package ipam

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/bits"
	"net"
	"sync"
	"time"

	"github/setera/pkg/network/ipam/store"
)

const (
	// reservedAtStart is how many addresses at the front of the range the IPAM
	// keeps back. The first is the network address. The second is the gateway
	// address, which the CNI convention reserves even though this datapath
	// routes through a link-local gateway and not through a bridge.
	reservedAtStart = 2

	// reservedAtEnd is the broadcast address.
	reservedAtEnd = 1

	// maxAddresses caps the range that the IPAM accepts, which is a /12. A
	// node PodCIDR is far smaller than this. The cap stops a configuration
	// mistake, such as a /0, from asking for a bitmap of many megabytes.
	maxAddresses = 1 << 20
)

var _ NodeIPAM = (*BitmapNodeIPAM)(nil)

// BitmapNodeIPAM gives out addresses from the node PodCIDR with a bitmap.
//
// It holds three indexes so that every lookup is a map read:
//
//   - byContainer maps a container ID to its allocation. This is the identity.
//   - byPod maps "namespace/pod" to a container ID. The reconcile pass and the
//     NodeStore mirror use it.
//   - byIP maps an address offset to a container ID, so a release does not have
//     to scan every allocation.
//
// The lock covers the store call as well. A write therefore waits for the disk.
// That is on purpose: the CNI must never get a reply that describes an address
// which a crash can lose. Pod creation happens tens of times per minute on a
// node, so one flush per change is not a bottleneck.
type BitmapNodeIPAM struct {
	mu sync.RWMutex

	subnet   *net.IPNet
	base     uint32 // the network address as an integer
	capacity int    // how many pod addresses the range holds
	bitmap   []uint64

	// cursor is where the next search starts. The search moves forward and
	// wraps, so a freed address is not handed straight back. An immediate
	// reuse can meet a stale ARP or conntrack entry for the old pod.
	cursor int

	// seq is the number for the next store record.
	seq uint64

	byContainer map[string]*Allocation
	byPod       map[string]string
	byIP        map[uint32]string

	store store.Store

	// now supplies the allocation timestamp. Tests replace it.
	now func() time.Time
}

// NewBitmapNodeIPAM builds the node IPAM over subnet and recovers the state
// that st already holds.
func NewBitmapNodeIPAM(ctx context.Context, subnet *net.IPNet, st store.Store) (*BitmapNodeIPAM, error) {
	if subnet == nil {
		return nil, ErrNilSubnet{}
	}
	if st == nil {
		return nil, ErrNilStore{}
	}

	ones, width := subnet.Mask.Size()
	if width != 32 {
		return nil, ErrIPv4OnlySupported{MaskSize: width}
	}
	base, ok := ipToUint32(subnet.IP.Mask(subnet.Mask))
	if !ok {
		return nil, ErrIPv4OnlySupported{MaskSize: width}
	}

	total := 1 << (width - ones)
	if total > maxAddresses {
		return nil, ErrSubnetTooLarge{Subnet: subnet.String(), Max: maxAddresses}
	}
	capacity := total - reservedAtStart - reservedAtEnd
	if capacity <= 0 {
		return nil, ErrSubnetTooSmall{Subnet: subnet.String()}
	}

	ipam := &BitmapNodeIPAM{
		subnet:      subnet,
		base:        base,
		capacity:    capacity,
		bitmap:      make([]uint64, (capacity+63)/64),
		seq:         1,
		byContainer: make(map[string]*Allocation),
		byPod:       make(map[string]string),
		byIP:        make(map[uint32]string),
		store:       st,
		now:         time.Now,
	}

	if err := ipam.recover(ctx); err != nil {
		return nil, err
	}
	return ipam, nil
}

// recover rebuilds the bitmap and the indexes from the store.
func (ipam *BitmapNodeIPAM) recover(ctx context.Context) error {
	state, err := ipam.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("ipam: load stored allocations: %w", err)
	}

	for containerID, alloc := range state.Allocations {
		idx, ok := ipam.ipToIndex(alloc.IP)
		if !ok {
			// The PodCIDR changed under a running node, or the state belongs
			// to another node. Either way the IPAM cannot account for the
			// address, so it must not start and hand it to a second pod.
			return ErrStoredIPOutOfRange{ContainerID: containerID, IP: alloc.IP, Subnet: ipam.subnet}
		}
		if other, taken := ipam.byIP[uint32(idx)]; taken {
			return ErrStoredIPConflict{IP: alloc.IP, ContainerIDs: [2]string{other, containerID}}
		}

		stored := alloc.Clone()
		ipam.setBit(idx)
		ipam.byContainer[containerID] = &stored
		ipam.byIP[uint32(idx)] = containerID
		ipam.byPod[podIndexKey(alloc.Namespace, alloc.PodName)] = containerID
	}

	// Continue past every record the store still accounts for, so that a new
	// record does not reuse the number of a live one.
	ipam.seq = state.LastSeq + 1
	return nil
}

// Allocate gives an address to the pod in req.
func (ipam *BitmapNodeIPAM) Allocate(ctx context.Context, req AllocationRequest) (*Allocation, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	ipam.mu.Lock()
	defer ipam.mu.Unlock()

	// A repeated ADD must return the first answer and not take a second
	// address.
	if existing, ok := ipam.byContainer[req.ContainerID]; ok {
		out := existing.Clone()
		return &out, nil
	}

	idx, ok := ipam.findFreeIndex()
	if !ok {
		return nil, ErrNoAvailableIPs{subnet: ipam.subnet.String()}
	}

	alloc := Allocation{
		TenantID:    req.TenantID,
		Namespace:   req.Namespace,
		PodName:     req.PodName,
		IFName:      req.IFName,
		NetNS:       req.NetNS,
		IP:          ipam.indexToIP(idx),
		Ifindex:     -1,
		AllocatedAt: ipam.now(),
	}

	// Reserve first, then commit to the disk. A failed write rolls the
	// reservation back, so a store outage does not leak an address.
	ipam.setBit(idx)
	if err := ipam.commit(ctx, store.Record{Op: store.OpPut, Key: req.ContainerID, Alloc: &alloc}); err != nil {
		ipam.clearBit(idx)
		return nil, err
	}

	ipam.byContainer[req.ContainerID] = &alloc
	ipam.byIP[uint32(idx)] = req.ContainerID
	// A pod sandbox restart points the pod key at the newest container. The
	// old allocation stays until its DEL arrives or the reconcile pass frees
	// it. Release guards the pod key, so the old DEL cannot take this entry.
	ipam.byPod[podIndexKey(req.Namespace, req.PodName)] = req.ContainerID
	ipam.cursor = idx + 1

	out := alloc.Clone()
	return &out, nil
}

// Release gives the address of containerID back.
func (ipam *BitmapNodeIPAM) Release(ctx context.Context, containerID string) error {
	if containerID == "" {
		return ErrEmptyContainerID{}
	}

	ipam.mu.Lock()
	defer ipam.mu.Unlock()

	alloc, ok := ipam.byContainer[containerID]
	if !ok {
		// The CNI calls DEL more than one time, and for pods that never
		// finished ADD.
		return nil
	}
	idx, inRange := ipam.ipToIndex(alloc.IP)

	// Remove the record first. A crash after this point leaves the address
	// marked as used, which only wastes it. The other order can hand a live
	// address to a second pod.
	if err := ipam.commit(ctx, store.Record{Op: store.OpDelete, Key: containerID}); err != nil {
		return err
	}

	if inRange {
		ipam.clearBit(idx)
		delete(ipam.byIP, uint32(idx))
	}
	delete(ipam.byContainer, containerID)

	// Drop the pod key only while it still points at this container. After a
	// sandbox restart it points at the new container, and a late DEL for the
	// old one must not take the entry of the new one.
	key := podIndexKey(alloc.Namespace, alloc.PodName)
	if ipam.byPod[key] == containerID {
		delete(ipam.byPod, key)
	}
	return nil
}

// SetHostVeth records the host side veth of an allocation.
func (ipam *BitmapNodeIPAM) SetHostVeth(ctx context.Context, containerID, hostVethName string, ifindex int) error {
	if containerID == "" {
		return ErrEmptyContainerID{}
	}

	ipam.mu.Lock()
	defer ipam.mu.Unlock()

	alloc, ok := ipam.byContainer[containerID]
	if !ok {
		return ErrAllocationNotFound{ContainerID: containerID}
	}

	// Build the new value first, so a failed write leaves the old one in
	// memory and on the disk.
	updated := alloc.Clone()
	updated.HostVethName = hostVethName
	updated.Ifindex = ifindex

	if err := ipam.commit(ctx, store.Record{Op: store.OpPut, Key: containerID, Alloc: &updated}); err != nil {
		return err
	}

	*alloc = updated
	return nil
}

// Get returns the allocation for a container ID.
func (ipam *BitmapNodeIPAM) Get(containerID string) (*Allocation, bool) {
	ipam.mu.RLock()
	defer ipam.mu.RUnlock()

	alloc, ok := ipam.byContainer[containerID]
	if !ok {
		return nil, false
	}
	out := alloc.Clone()
	return &out, true
}

// GetByPod returns the newest allocation for a pod.
func (ipam *BitmapNodeIPAM) GetByPod(namespace, podName string) (*Allocation, bool) {
	ipam.mu.RLock()
	defer ipam.mu.RUnlock()

	containerID, ok := ipam.byPod[podIndexKey(namespace, podName)]
	if !ok {
		return nil, false
	}
	alloc, ok := ipam.byContainer[containerID]
	if !ok {
		return nil, false
	}
	out := alloc.Clone()
	return &out, true
}

// List returns every live allocation.
func (ipam *BitmapNodeIPAM) List() map[string]*Allocation {
	ipam.mu.RLock()
	defer ipam.mu.RUnlock()
	return ipam.snapshot("")
}

// ListByTenant returns the live allocations of one tenant.
func (ipam *BitmapNodeIPAM) ListByTenant(tenantID string) map[string]*Allocation {
	ipam.mu.RLock()
	defer ipam.mu.RUnlock()
	return ipam.snapshot(tenantID)
}

// snapshot copies the allocations. The caller gets its own copies, so it cannot
// change the IPAM state by writing to the result. Callers must hold the lock.
func (ipam *BitmapNodeIPAM) snapshot(tenantID string) map[string]*Allocation {
	out := make(map[string]*Allocation, len(ipam.byContainer))
	for containerID, alloc := range ipam.byContainer {
		if tenantID != "" && alloc.TenantID != tenantID {
			continue
		}
		copied := alloc.Clone()
		out[containerID] = &copied
	}
	return out
}

// Subnet reports the node PodCIDR.
func (ipam *BitmapNodeIPAM) Subnet() *net.IPNet {
	ipam.mu.RLock()
	defer ipam.mu.RUnlock()

	out := &net.IPNet{
		IP:   append(net.IP(nil), ipam.subnet.IP...),
		Mask: append(net.IPMask(nil), ipam.subnet.Mask...),
	}
	return out
}

// Capacity reports how many pod addresses the range holds.
func (ipam *BitmapNodeIPAM) Capacity() int {
	ipam.mu.RLock()
	defer ipam.mu.RUnlock()
	return ipam.capacity
}

// Remaining reports how many addresses are still free.
func (ipam *BitmapNodeIPAM) Remaining() int {
	ipam.mu.RLock()
	defer ipam.mu.RUnlock()

	used := 0
	for _, word := range ipam.bitmap {
		used += bits.OnesCount64(word)
	}
	if used > ipam.capacity {
		used = ipam.capacity
	}
	return ipam.capacity - used
}

// Close releases the store.
func (ipam *BitmapNodeIPAM) Close() error {
	ipam.mu.Lock()
	defer ipam.mu.Unlock()
	return ipam.store.Close()
}

// commit numbers a record and writes it. Callers must hold the write lock, so
// that the numbers stay in order.
func (ipam *BitmapNodeIPAM) commit(ctx context.Context, rec store.Record) error {
	rec.Seq = ipam.seq
	ipam.seq++
	return ipam.store.Apply(ctx, rec)
}

// findFreeIndex searches forward from the cursor and wraps once. Callers must
// hold the write lock.
func (ipam *BitmapNodeIPAM) findFreeIndex() (int, bool) {
	if ipam.capacity == 0 {
		return 0, false
	}
	start := ipam.cursor
	if start < 0 || start >= ipam.capacity {
		start = 0
	}

	for offset := 0; offset < ipam.capacity; offset++ {
		idx := start + offset
		if idx >= ipam.capacity {
			idx -= ipam.capacity
		}
		if !ipam.testBit(idx) {
			return idx, true
		}
	}
	return 0, false
}

func (ipam *BitmapNodeIPAM) testBit(idx int) bool {
	return ipam.bitmap[idx/64]&(1<<uint(idx%64)) != 0
}

func (ipam *BitmapNodeIPAM) setBit(idx int) {
	ipam.bitmap[idx/64] |= 1 << uint(idx%64)
}

func (ipam *BitmapNodeIPAM) clearBit(idx int) {
	ipam.bitmap[idx/64] &^= 1 << uint(idx%64)
}

// indexToIP maps a bitmap index to an address. Index 0 is the first address
// after the reserved ones at the front of the range.
func (ipam *BitmapNodeIPAM) indexToIP(idx int) net.IP {
	return uint32ToIP(ipam.base + uint32(reservedAtStart) + uint32(idx))
}

// ipToIndex maps an address to a bitmap index. It reports false when the
// address falls outside the pod range.
func (ipam *BitmapNodeIPAM) ipToIndex(ip net.IP) (int, bool) {
	value, ok := ipToUint32(ip)
	if !ok {
		return 0, false
	}
	if value < ipam.base+uint32(reservedAtStart) {
		return 0, false
	}
	idx := int(value - ipam.base - uint32(reservedAtStart))
	if idx >= ipam.capacity {
		return 0, false
	}
	return idx, true
}

// validate reports whether the request holds what an allocation needs.
func (r AllocationRequest) validate() error {
	if r.ContainerID == "" {
		return ErrEmptyContainerID{}
	}
	if r.TenantID == "" {
		return ErrEmptyTenantID{ContainerID: r.ContainerID}
	}
	if r.PodName == "" {
		return ErrEmptyPodName{ContainerID: r.ContainerID}
	}
	return nil
}

// podIndexKey builds the secondary index key for a pod.
func podIndexKey(namespace, podName string) string {
	if namespace == "" {
		return podName
	}
	return namespace + "/" + podName
}

// ipToUint32 converts an IPv4 address to an integer.
func ipToUint32(ip net.IP) (uint32, bool) {
	v4 := ip.To4()
	if v4 == nil {
		return 0, false
	}
	return binary.BigEndian.Uint32(v4), true
}

// uint32ToIP converts an integer back to an IPv4 address.
func uint32ToIP(value uint32) net.IP {
	out := make(net.IP, net.IPv4len)
	binary.BigEndian.PutUint32(out, value)
	return out
}
