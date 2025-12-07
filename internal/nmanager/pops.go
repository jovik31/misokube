package nmanager

import (
	"context"
)

var _ PodOps = (*NetworkManagerImpl)(nil)

// Pod lifecycle operations exposed to CNI path via tenant actors.
func (nm *NetworkManagerImpl) AllocatePod(ctx context.Context, tenantID, epKey string) error {
	// Perform actual network attach using managers and tenant record.
	nm.mu.RLock()
	rec, ok := nm.TenantRecords[tenantID]
	nm.mu.RUnlock()
	if !ok || rec == nil {
		return ErrTenantActorNotFound
	}
	// TODO: allocate IP via rec.IPAM, create/attach veth via rec.Backend, program routes via nm.Route.
	return nil
}

func (nm *NetworkManagerImpl) RemovePod(ctx context.Context, tenantID, epKey string) error {
	nm.mu.RLock()
	rec, ok := nm.TenantRecords[tenantID]
	nm.mu.RUnlock()
	if !ok || rec == nil {
		return ErrTenantActorNotFound
	}
	// TODO: detach veth, release IP via rec.IPAM, cleanup routes via nm.Route.
	return nil
}
