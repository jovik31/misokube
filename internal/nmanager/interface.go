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

// NodestoreOps are operations related to the remote node store of tenant network state.
// Implementations should be thread-safe and idempotent.
type NodestoreOps interface {

	// EnsureRemoteTenantConn ensures that the node has connectivity to the remote tenant network.
	EnsureRemoteTenantConn(ctx context.Context, tenantID, nodeName string) error

	// UpdateRemoteTenantConn updates connectivity to the remote tenant network, e.g. after a config change.
	UpdateRemoteTenantConn(ctx context.Context, tenantID, nodeName string) error

	// RemoveRemoteTenantConn removes connectivity to the remote tenant network.
	RemoveRemoteTenantConn(ctx context.Context, tenantID, nodeName string) error
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
