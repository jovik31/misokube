package router

import (
	"context"
	"errors"
)

// TenantActor is the minimal interface the router needs from a tenant actor.
// Implemented on the network manager side.
type TenantActor interface {
	EnsurePod(ctx context.Context, args PodAttachArgs) error
	RemovePod(ctx context.Context, args PodAttachArgs) error
}

// LookupFunc returns a tenant actor for a given tenantID if it exists.
type LookupFunc func(tenantID string) (TenantActor, error)

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
func (r *NManagerRouter) ConfigurePod(ctx context.Context, tenantID string, podUID string, args PodAttachArgs) (CNIResult, error) {
	if r.lookup == nil {
		return CNIResult{}, errors.New("router not initialized: no lookup function")
	}
	actor, err := r.lookup(tenantID)
	if err != nil || actor == nil {
		return CNIResult{}, errors.New("tenant actor not found")
	}
	if err := actor.EnsurePod(ctx, args); err != nil {
		return CNIResult{}, err
	}
	// TODO: Populate CNIResult from nm state (IP, routes, DNS). Placeholder minimal result for now.
	return CNIResult{IfName: args.IfName}, nil
}
