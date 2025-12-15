package nmanager

import (
	"context"
	"errors"

	"github/setera/internal/router"
)

// TenantActor defines minimal pod lifecycle operations for a tenant.
type TenantActor interface {
	EnsurePod(ctx context.Context, args router.PodAttachArgs) error
	RemovePod(ctx context.Context, args router.PodAttachArgs) error
	UpdatePod(ctx context.Context, args router.PodAttachArgs) error
	Stop(ctx context.Context) error
}

var ErrTenantActorNotFound = errors.New("tenant actor not found")
var ErrTenantClosing = errors.New("tenant closing")

// getTenantActor returns the tenant actor if present.
// getTenantActor returns the tenant actor if present (internal).
func (m *NetworkManagerImpl) getTenantActor(tenantID string) (TenantActor, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.TenantActors == nil {
		return nil, ErrTenantActorNotFound
	}

	a, ok := m.TenantActors[tenantID]
	if !ok {
		return nil, ErrTenantActorNotFound
	}
	return a, nil
}

// GetTenantActor exposes read-only access to registered tenant actors.
func (m *NetworkManagerImpl) GetTenantActor(tenantID string) (TenantActor, error) {

	ta, err := m.getTenantActor(tenantID)
	if err != nil {
		return nil, err
	}
	return ta, nil
}
