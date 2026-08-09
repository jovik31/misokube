// Package ipam defines generic IP address allocation primitives.
//
// The package is intentionally independent from persistence, CNI, Kubernetes,
// workload identity, and any specific networking system. An Allocator manages
// address occupancy only.
//
// Implementations must be safe for concurrent use unless documented otherwise.
package ipam

import "net/netip"

// Allocator manages addresses from one logical pool.
//
// Allocate returns one currently free address and marks it allocated.
// AllocateSpecific marks a caller-selected address allocated. It is useful for
// restoration and callers that require a particular address.
// Release makes an allocated address available again. Releasing an address that
// is already free is idempotent and returns nil.
// IsAllocated reports whether an address is currently allocated.
// Stats returns a point-in-time view of pool utilization.
type Allocator interface {
	Allocate() (netip.Addr, error)
	AllocateSpecific(addr netip.Addr) error
	Release(addr netip.Addr) error
	IsAllocated(addr netip.Addr) (bool, error)
	Stats() Stats
}

// Stats describes allocator utilization.
//
// Capacity excludes addresses that the concrete allocator has permanently
// reserved from allocation.
type Stats struct {
	Capacity  uint64
	Allocated uint64
	Available uint64
}
