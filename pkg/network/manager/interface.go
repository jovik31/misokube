package manager

import (
    "context"
    "net"
)

// NetworkManager exposes tenant-scoped admin operations and per-tenant pod operations.
// This interface is deliberately K8s-agnostic; higher layers (operators) translate CRs to these calls.
type NetworkManager interface {
    TenantOps
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
    // ReconcileAll triggers a best-effort reconciliation of all tenants.
    ReconcileAll(ctx context.Context) error
}

// PodOps are per-tenant, synchronous operations typically invoked by the CNI path via a Tenant Actor.
// Provide both composite and low-level primitives. All methods must be idempotent.
type PodOps interface {
    // Composite operations (preferred for CNI):
    // AllocateNet allocates an IP for the endpoint identified by epKey and attaches it in one transaction.
    AllocateNet(ctx context.Context, tenantID, epKey string) (EndpointView, error)
    // RemoveNet detaches the endpoint (if present) and releases the IP (if allocated).
    RemoveNet(ctx context.Context, tenantID, epKey string) error

    // Low-level operations:
    AllocateIP(ctx context.Context, tenantID, epKey string) (net.IP, error)
    ReleaseIP(ctx context.Context, tenantID, epKey string) error
    AttachEndpoint(ctx context.Context, tenantID, epKey string) error
    DetachEndpoint(ctx context.Context, tenantID, epKey string) error

    // SnapshotTenant returns a read-only view of the current tenant runtime state.
    SnapshotTenant(tenantID string) (TenantSnapshot, bool)
}

// TenantConfig captures the inputs required to realize a tenant network.
// Intentionally generic and K8s-agnostic; operators map CR Spec to this shape.
// Note: Desired config and endpoint metadata should be retrievable by the implementation
// via injected stores/resolvers keyed by tenantID and epKey. The API intentionally avoids
// config or endpoint structs as inputs to keep the interface minimal and K8s-agnostic.

// Read-only view of an attached endpoint.
type EndpointView struct {
    IfName string
    IP     net.IP
}

// Read-only snapshot of tenant runtime state.
type TenantSnapshot struct {
    TenantID   string
    CIDR       *net.IPNet
    BridgeName string
    VTEPName   string
    VNI        int
    AllocCount int
    Endpoints  map[string]EndpointView
}
