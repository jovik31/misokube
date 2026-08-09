package ipam

import (
	"context"
	"net"

	"github/setera/pkg/network/ipam/store"
)

// Allocation is the network state of one pod.
//
// It is the same type that the store keeps on the disk. The in-memory shape and
// the durable shape must not drift apart, because a field that only one of them
// carries is a field that a restart loses.
type Allocation = store.Allocation

// AllocationRequest holds what the CNI knows when it asks for an address.
type AllocationRequest struct {
	// ContainerID identifies the allocation. It is the key that the CNI uses
	// for ADD and DEL, so a new pod sandbox gets a new allocation and cannot
	// collide with a record that an old sandbox left behind.
	ContainerID string

	// TenantID is the tenant that owns the pod. Every tenant on the node
	// shares one PodCIDR, so the tenant labels the allocation and the eBPF
	// policy layer keeps the tenants apart.
	TenantID string

	// Namespace and PodName identify the pod to Kubernetes. The IPAM indexes
	// them, so that the NodeStore mirror and the reconcile pass can find an
	// allocation without a container ID.
	Namespace string
	PodName   string

	// IFName is the interface name inside the pod network namespace.
	IFName string

	// NetNS is the path of the pod network namespace.
	NetNS string
}

// NodeIPAM gives out pod addresses from the node PodCIDR.
//
// One instance serves the whole node. Every tenant draws from the same range.
//
// Every method that changes the state writes to the store before it returns, so
// a crash cannot lose an address that a live pod holds.
type NodeIPAM interface {
	// Allocate gives an address to the pod in the request. It is idempotent:
	// a second call with the same container ID returns the first allocation
	// and does not take a second address. The CNI repeats an ADD after a
	// timeout, so this behaviour is required.
	Allocate(ctx context.Context, req AllocationRequest) (*Allocation, error)

	// Release gives the address back. It is idempotent: an unknown container
	// ID reports success, because the CNI calls DEL more than one time and
	// also for a pod that never finished ADD.
	Release(ctx context.Context, containerID string) error

	// SetHostVeth records the host side of the veth pair. The eBPF layer
	// attaches to that interface, so the name and the index must survive a
	// restart. Pass an index of -1 when it is not known yet.
	SetHostVeth(ctx context.Context, containerID, hostVethName string, ifindex int) error

	// Get returns the allocation for a container ID.
	Get(containerID string) (*Allocation, bool)

	// GetByPod returns the allocation for a namespace and pod name. When a
	// pod sandbox restarts, it returns the newest allocation for that pod.
	GetByPod(namespace, podName string) (*Allocation, bool)

	// List returns every live allocation, keyed by container ID.
	List() map[string]*Allocation

	// ListByTenant returns the live allocations of one tenant, keyed by
	// container ID.
	ListByTenant(tenantID string) map[string]*Allocation

	// Subnet reports the node PodCIDR that this IPAM serves.
	Subnet() *net.IPNet

	// Capacity reports how many pod addresses the PodCIDR holds.
	Capacity() int

	// Remaining reports how many addresses are still free.
	Remaining() int

	// Close releases the store.
	Close() error
}
