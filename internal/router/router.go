package router

import (
    "context"
)

// Meta holds minimal pod-related metadata needed for configuration.
// Extend as needed to match CNI handler expectations.
type Meta struct {
    Namespace   string
    PodName     string
    ContainerID string
    NetNS       string
    IfName      string
}

// CNIResult is the router's response containing fields required
// to satisfy the CNI ADD command result.
// Keep aligned with pkg/cni expectations.
type CNIResult struct {
    IP4        string
    Gateway4   string
    DNSNamesrv []string
    Routes4    []string
    IfName     string
}

// Router is the interface between CNIServer and the network/tenant actors.
// It assumes tenant actors already exist (created via cluster-driven ensureTenant),
// and will return an error if the tenant actor is not found.
type Router interface {
    ConfigurePod(ctx context.Context, tenantID string, podUID string, meta Meta) (CNIResult, error)
}
