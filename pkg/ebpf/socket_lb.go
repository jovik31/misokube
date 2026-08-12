package ebpf

import (
	"fmt"

	"github/setera/pkg/ebpf/loader"
)

// SocketLB owns Setera's IPv4 cgroup socket Service load balancer.
//
// The socket load balancer rewrites ClusterIP TCP and UDP destinations to a
// ready backend before packets enter the normal network stack. The existing TC
// Pod policy then enforces tenant isolation against the selected backend.
//
// kube-proxy remains enabled as the fallback for Service traffic that Setera
// does not translate.
type SocketLB struct {
	handle *loader.SocketLB
}

// AttachSocketLB attaches the Service socket load balancer to cgroupRoot.
func AttachSocketLB(cgroupRoot string) (*SocketLB, error) {
	handle, err := loader.NewSocketLB(cgroupRoot)
	if err != nil {
		return nil, fmt.Errorf("ebpf: attach Service socket LB: %w", err)
	}

	return &SocketLB{handle: handle}, nil
}

// Close detaches the Service socket load balancer.
func (lb *SocketLB) Close() error {
	if lb == nil || lb.handle == nil {
		return nil
	}
	if err := lb.handle.Close(); err != nil {
		return fmt.Errorf("ebpf: close Service socket LB: %w", err)
	}
	return nil
}
