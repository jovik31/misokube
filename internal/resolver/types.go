package resolver

import "context"

type Resolver interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
	Ready() bool
	Resolve(namespace, podName, uid string) (tenantID string, retryable bool, err error)
}

type Snapshot struct {
	ByUID  map[string]string // uid -> tenant
	ByName map[string]string // ns/pod -> tenant
	Synced bool
}
