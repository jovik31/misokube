package ebpfmanager

import (
	"context"
)

// Manager is the main interface for managing eBPF programs and maps.
// This manager updates existing maps and loads programs into pod interfaces
type EbpfManager interface {
	TenantOps
	PodOps
}

type TenantOps interface {
	//EnsureTenantMaps creates or updates tenant-specific eBPF maps. Idempotent and thread-safe.
	EnsureTenantMaps(ctx context.Context, tenantID string) error

	//Não sei se vai ser preciso porque o mapa deve desaparecer quando o pod for apagado
	//RemoveTenantMaps deletes tenant-specific eBPF maps. Safe to call multiple times.
	RemoveTenantMaps(ctx context.Context, tenantID string) error
}

type PodOps interface {
	//EnsurePodEndpoint attaches or updates eBPF programs for the given pod. Idempotent.
	EnsurePodEndpoint(ctx context.Context, tenantID string, podName string, ifName string) error

	UpdatePodEndpoint(ctx context.Context, tenantID string, podName string) error

	//RemovePodEndpoint detaches eBPF programs for the given pod. Safe to call multiple times.
	RemovePodEndpoint(ctx context.Context, tenantID string, podName string) error

	// UpsertPodMapEntry writes/updates a destination pod entry in tc_podIDs.
	UpsertPodMapEntry(ctx context.Context, tenantID string, podName string, podIP string, ifindex int) error

	// DeletePodMapEntry removes a destination pod entry from tc_podIDs.
	DeletePodMapEntry(ctx context.Context, podIP string) error
}
