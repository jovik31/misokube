// Package bitmap provides an in-memory bitmap-backed IP address allocator.
//
// The allocator supports IPv4 and IPv6 prefixes whose address count fits within
// the configured maximum. It is intended for bounded local pools where bitmap
// allocation provides predictable memory use and fast occupancy checks.
package bitmap

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"sync"

	"github/setera/pkg/ipam"
)

const defaultMaxAddresses uint64 = 1 << 20 // 1,048,576 addresses; 128 KiB per bitmap.

// Option configures an Allocator.
type Option func(*config) error

type config struct {
	maxAddresses uint64
	reserved     []netip.Addr
}

// WithMaxAddresses sets the largest pool this bitmap implementation accepts.
// It prevents accidental construction of impractically large bitmaps, such as
// an IPv6 /64. n must be greater than zero.
func WithMaxAddresses(n uint64) Option {
	return func(c *config) error {
		if n == 0 {
			return fmt.Errorf("bitmap: max addresses must be greater than zero")
		}
		c.maxAddresses = n
		return nil
	}
}

// WithReserved marks addresses as permanently unavailable to Allocate and
// AllocateSpecific. Reserved addresses must belong to the configured prefix.
func WithReserved(addrs ...netip.Addr) Option {
	return func(c *config) error {
		c.reserved = append(c.reserved, addrs...)
		return nil
	}
}

// Allocator manages dynamic and permanently reserved addresses using bitmaps.
type Allocator struct {
	prefix   netip.Prefix
	base     netip.Addr
	capacity uint64

	mu        sync.RWMutex
	allocated []uint64
	reserved  []uint64
	used      uint64
	reservedN uint64
	next      uint64
}

// New creates a bitmap allocator for prefix.
//
// The prefix is masked before use. Pools larger than the configured maximum are
// rejected before any bitmap allocation occurs.
func New(prefix netip.Prefix, opts ...Option) (*Allocator, error) {
	if !prefix.IsValid() {
		return nil, ipam.ErrInvalidPrefix
	}

	prefix = prefix.Masked()
	cfg := config{maxAddresses: defaultMaxAddresses}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}

	capacity, err := prefixCapacity(prefix, cfg.maxAddresses)
	if err != nil {
		return nil, err
	}

	words := (capacity + 63) / 64
	if words > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("bitmap: %d words: %w", words, ipam.ErrPoolTooLarge)
	}

	a := &Allocator{
		prefix:    prefix,
		base:      prefix.Addr(),
		capacity:  capacity,
		allocated: make([]uint64, int(words)),
		reserved:  make([]uint64, int(words)),
	}

	for _, addr := range cfg.reserved {
		idx, err := a.indexOf(addr)
		if err != nil {
			return nil, fmt.Errorf("bitmap: reserve %s: %w", addr, err)
		}
		if bitSet(a.reserved, idx) {
			continue
		}
		setBit(a.reserved, idx)
		a.reservedN++
	}

	return a, nil
}

// Prefix returns the normalized pool prefix.
func (a *Allocator) Prefix() netip.Prefix {
	return a.prefix
}

// Allocate returns a free address and marks it allocated.
func (a *Allocator) Allocate() (netip.Addr, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.used+a.reservedN >= a.capacity {
		return netip.Addr{}, ipam.ErrExhausted
	}

	idx, ok := a.findFree(a.next)
	if !ok {
		return netip.Addr{}, ipam.ErrExhausted
	}

	setBit(a.allocated, idx)
	a.used++
	a.next = (idx + 1) % a.capacity
	return a.addrAt(idx), nil
}

// AllocateSpecific allocates addr if it belongs to the pool and is free.
func (a *Allocator) AllocateSpecific(addr netip.Addr) error {
	idx, err := a.indexOf(addr)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if bitSet(a.reserved, idx) {
		return ipam.ErrReserved
	}
	if bitSet(a.allocated, idx) {
		return ipam.ErrAllocated
	}

	setBit(a.allocated, idx)
	a.used++
	return nil
}

// Release releases addr. Releasing an already-free address is idempotent.
func (a *Allocator) Release(addr netip.Addr) error {
	idx, err := a.indexOf(addr)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if bitSet(a.reserved, idx) {
		return ipam.ErrReserved
	}
	if !bitSet(a.allocated, idx) {
		return nil
	}

	clearBit(a.allocated, idx)
	a.used--
	if idx < a.next {
		a.next = idx
	}
	return nil
}

// IsAllocated reports whether addr is dynamically allocated.
func (a *Allocator) IsAllocated(addr netip.Addr) (bool, error) {
	idx, err := a.indexOf(addr)
	if err != nil {
		return false, err
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	return bitSet(a.allocated, idx), nil
}

// Stats returns a point-in-time pool utilization snapshot.
func (a *Allocator) Stats() ipam.Stats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	capacity := a.capacity - a.reservedN
	return ipam.Stats{
		Capacity:  capacity,
		Allocated: a.used,
		Available: capacity - a.used,
	}
}

func (a *Allocator) findFree(start uint64) (uint64, bool) {
	if a.capacity == 0 {
		return 0, false
	}

	for scanned := uint64(0); scanned < a.capacity; scanned++ {
		idx := start + scanned
		if idx >= a.capacity {
			idx -= a.capacity
		}
		if !bitSet(a.allocated, idx) && !bitSet(a.reserved, idx) {
			return idx, true
		}
	}
	return 0, false
}

func (a *Allocator) indexOf(addr netip.Addr) (uint64, error) {
	if !addr.IsValid() {
		return 0, ipam.ErrInvalidAddress
	}
	if !a.prefix.Contains(addr) {
		return 0, ipam.ErrOutOfRange
	}

	if a.base.Is4() {
		base := a.base.As4()
		value := addr.As4()
		return uint64(binary.BigEndian.Uint32(value[:]) - binary.BigEndian.Uint32(base[:])), nil
	}

	base := a.base.As16()
	value := addr.As16()

	// New rejects pools with 63 or more host bits, so any valid address in this
	// allocator differs from the base only in the low 64 bits.
	if binary.BigEndian.Uint64(value[:8]) != binary.BigEndian.Uint64(base[:8]) {
		return 0, ipam.ErrOutOfRange
	}
	return binary.BigEndian.Uint64(value[8:]) - binary.BigEndian.Uint64(base[8:]), nil
}

func (a *Allocator) addrAt(idx uint64) netip.Addr {
	if a.base.Is4() {
		base := a.base.As4()
		value := binary.BigEndian.Uint32(base[:]) + uint32(idx)
		var out [4]byte
		binary.BigEndian.PutUint32(out[:], value)
		return netip.AddrFrom4(out)
	}

	base := a.base.As16()
	value := binary.BigEndian.Uint64(base[8:]) + idx
	binary.BigEndian.PutUint64(base[8:], value)
	return netip.AddrFrom16(base)
}

func prefixCapacity(prefix netip.Prefix, maxAddresses uint64) (uint64, error) {
	bitLen := prefix.Addr().BitLen()
	if bitLen != 32 && bitLen != 128 {
		return 0, ipam.ErrInvalidPrefix
	}

	hostBits := bitLen - prefix.Bits()
	if hostBits < 0 {
		return 0, ipam.ErrInvalidPrefix
	}

	// Shifting by 64 or more cannot be represented in uint64 and is far beyond
	// a practical bitmap pool anyway.
	if hostBits >= 64 {
		return 0, fmt.Errorf("bitmap: prefix %s: %w", prefix, ipam.ErrPoolTooLarge)
	}

	capacity := uint64(1) << hostBits
	if capacity > maxAddresses {
		return 0, fmt.Errorf(
			"bitmap: prefix %s contains %d addresses, maximum is %d: %w",
			prefix,
			capacity,
			maxAddresses,
			ipam.ErrPoolTooLarge,
		)
	}
	return capacity, nil
}

func bitSet(bitmap []uint64, idx uint64) bool {
	word := idx / 64
	bit := idx % 64
	return bitmap[word]&(uint64(1)<<bit) != 0
}

func setBit(bitmap []uint64, idx uint64) {
	word := idx / 64
	bit := idx % 64
	bitmap[word] |= uint64(1) << bit
}

func clearBit(bitmap []uint64, idx uint64) {
	word := idx / 64
	bit := idx % 64
	bitmap[word] &^= uint64(1) << bit
}
