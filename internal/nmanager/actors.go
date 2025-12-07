package nmanager

import (
	"context"
	"errors"
)

// TenantActor defines minimal pod lifecycle operations for a tenant.
type TenantActor interface {
	AllocatePod(ctx context.Context, epKey string) error
	RemovePod(ctx context.Context, epKey string) error
	Stop(ctx context.Context) error
}

var ErrTenantActorNotFound = errors.New("tenant actor not found")

// getTenantActor returns the tenant actor if present.
// getTenantActor returns the tenant actor if present (internal).
func (m *NetworkManagerImpl) getTenantActor(tenantID string) (TenantActor, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.TenantActors == nil {
		return nil, false
	}
	a, ok := m.TenantActors[tenantID]
	return a, ok
}

// GetTenantActor exposes read-only access to registered tenant actors.
func (m *NetworkManagerImpl) GetTenantActor(tenantID string) (TenantActor, bool) {
	return m.getTenantActor(tenantID)
}
