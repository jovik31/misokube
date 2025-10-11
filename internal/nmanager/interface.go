package nmanager

import (
	"context"
	"net"
)

// NetworkManager exposes tenant-scoped admin operations and per-tenant pod operations.
// This interface is deliberately K8s-agnostic; higher layers (operators) translate CRs to these calls.
type NetworkManager interface {
	TenantOps
	NodestoreOps
	PodOps
}

// TenantOps are administrative operations on tenants, independent of any K8s CR types.
// Implementations should be thread-safe and idempotent.
type TenantOps interface {

	// EnsureTenant creates or updates tenant network resources for the given tenantID.
	// Implementations retrieve any necessary desired state internally.
	EnsureTenant(ctx context.Context, tenantID string) error

	// RemoveTenant tears down tenant network resources. Safe to call multiple times.
	RemoveTenant(ctx context.Context, tenantID string) error
}

// NodeStoreOps exposes inter-node wiring primitives and local snapshots for a tenant.
// Synchronous and idempotent; used by the NodeStore operator.
type NodestoreOps interface {
	// SnapshotTenantInfra returns a read-only view of local tenant infra.
	SnapshotTenantInfra(tenantID string) (TenantInfraSnapshot, error)

	// EnsurePeer programs ARP, FDB, and route entries on the local VTEP towards a remote node.
	EnsurePeer(ctx context.Context, tenantID string, remote RemoteTenantInfra) error

	// RemovePeer removes ARP, FDB, and route entries towards a specific remote node.
	RemovePeer(ctx context.Context, tenantID string, remote RemoteTenantInfra) error

	// FlushTenant flushes ARP/FDB/routes for the tenant on local devices (used on teardown).
	FlushTenant(ctx context.Context, tenantID string) error
}

// PodOps are per-tenant, synchronous operations typically invoked by the CNI path via a Tenant Actor.
// Provide both composite and low-level primitives. All methods must be idempotent.
type PodOps interface {

	// AllocateNet allocates an IP for the endpoint identified by epKey and attaches it in one transaction.
	AllocateNet(ctx context.Context, tenantID, epKey string) error

	// RemoveNet detaches the endpoint (if present) and releases the IP (if allocated).
	RemoveNet(ctx context.Context, tenantID, epKey string) error

	// Low-level operations:
	AllocateIP(ctx context.Context, tenantID, epKey string) (net.IP, error)
	ReleaseIP(ctx context.Context, tenantID, epKey string) error
	AttachEndpoint(ctx context.Context, tenantID, epKey string) error
	DetachEndpoint(ctx context.Context, tenantID, epKey string) error
}

// TenantInfraSnapshot is the local tenant network snapshot used by the NodeStore operator.
type TenantInfraSnapshot struct {
	Subnet  *net.IPNet
	VNI     uint32
	VTEPDev string
	VTEPIP  net.IP
	VTEPMAC net.HardwareAddr
	MTU     int
	Bridge  string // optional: local bridge name
}

// RemoteTenantInfra describes the remote node’s tenant attributes needed to program ARP/FDB/routes.
type RemoteTenantInfra struct {
	NodeName string
	NodeIP   net.IP
	VTEPIP   net.IP
	VTEPMAC  net.HardwareAddr
	Subnet   *net.IPNet
	VNI      uint32
}
