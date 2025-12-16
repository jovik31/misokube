package router

import (
	"context"
	"errors"

	types100 "github.com/containernetworking/cni/pkg/types/100"
)

// check if router implementation satisfies router interface
var _ Router = (*NManagerRouter)(nil)

// TenantActor is the minimal interface the router needs from a tenant actor.
// Implemented on the network manager side.
type TenantActor interface {
	EnsurePod(ctx context.Context, args PodAttachArgs) (*types100.Result, error)
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
func (r *NManagerRouter) ConfigurePod(ctx context.Context, tenantID string, podUID string, args PodAttachArgs) (*types100.Result, error) {
	if r.lookup == nil {
		return nil, errors.New("router not initialized: no lookup function")
	}
	actor, err := r.lookup(tenantID)
	if err != nil || actor == nil {
		return nil, errors.New("tenant actor not found")
	}
	result, err := actor.EnsurePod(ctx, args)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RemovePod forwards DEL semantics to the tenant actor.
func (r *NManagerRouter) RemovePod(ctx context.Context, tenantID string, podUID string, args PodAttachArgs) error {
	if r.lookup == nil {
		return errors.New("router not initialized: no lookup function")
	}
	actor, err := r.lookup(tenantID)
	if err != nil || actor == nil {
		return errors.New("tenant actor not found")
	}
	return actor.RemovePod(ctx, args)
}
