package router

import (
	"context"
	"errors"
)

// TenantActor is the minimal interface the router needs from a tenant actor.
// Implemented on the network manager side.
type TenantActor interface {
	AllocatePod(ctx context.Context, epKey string) error
	RemovePod(ctx context.Context, epKey string) error
}

// LookupFunc returns a tenant actor for a given tenantID if it exists.
type LookupFunc func(tenantID string) (TenantActor, bool)

// NManagerRouter routes requests to tenant actors owned by the network manager.
// It does NOT create tenants; it only looks up existing actors.
type NManagerRouter struct {
	lookup LookupFunc
}

// NewNManagerRouter constructs a router using the provided actor lookup function.
func NewNManagerRouter(lookup LookupFunc) *NManagerRouter {
	return &NManagerRouter{lookup: lookup}
}

// ConfigurePod forwards the request to the tenant actor corresponding to tenantID.
// Currently supports ADD-style allocation; DEL should call RemovePod via a separate router method.
func (r *NManagerRouter) ConfigurePod(ctx context.Context, tenantID string, podUID string, meta Meta) (CNIResult, error) {
	if r.lookup == nil {
		return CNIResult{}, errors.New("router not initialized: no lookup function")
	}
	actor, ok := r.lookup(tenantID)
	if !ok || actor == nil {
		return CNIResult{}, errors.New("tenant actor not found")
	}
	// For now, use podUID as the endpoint key.
	if err := actor.AllocatePod(ctx, podUID); err != nil {
		return CNIResult{}, err
	}
	// TODO: Populate CNIResult from nm state (IP, routes, DNS). Placeholder minimal result for now.
	return CNIResult{IfName: meta.IfName}, nil
}
