package ebpf

import (
	"fmt"
	"strings"
	"sync"

	"github/setera/pkg/ebpf/loader"
)

const maxTenantIDBytes = 63

// PodProgram is an attached Setera TC program for one local Pod host veth.
//
// PodProgram owns only the low-level eBPF attachment. Kubernetes and Pod
// lifecycle state belong to internal/ebpfmanager.
type PodProgram struct {
	closeOnce sync.Once
	closeErr  error

	handle podProgramHandle
}

// AttachPodProgram loads the Setera Pod TC policy, configures its tenant
// identity, and attaches it to ingress and egress on the host-side veth.
//
// The tenant identity is stored in the BPF program's fixed char[64]
// my_tenant variable, leaving one byte for the terminating zero. The loader
// also sets my_is_default so the hot path can identify the privileged default
// source tenant with one scalar read instead of comparing the source string.
func AttachPodProgram(
	ifName string,
	tenant string,
) (*PodProgram, error) {
	return attachPodProgram(ifName, tenant, func(ifName, tenant string) (podProgramHandle, error) {
		return loader.NewPodPolicy(ifName, tenant)
	})
}

// Close detaches the Pod TC program and releases its eBPF resources.
// It is safe to call Close more than once.
func (p *PodProgram) Close() error {
	if p == nil {
		return nil
	}

	p.closeOnce.Do(func() {
		if p.handle != nil {
			p.closeErr = p.handle.Close()
		}
	})

	return p.closeErr
}

type podProgramHandle interface {
	Close() error
}

type podProgramAttacher func(
	ifName string,
	tenant string,
) (podProgramHandle, error)

func attachPodProgram(
	ifName string,
	tenant string,
	attach podProgramAttacher,
) (*PodProgram, error) {
	if strings.TrimSpace(ifName) == "" {
		return nil, fmt.Errorf("ebpf: pod program interface name is empty")
	}

	if strings.TrimSpace(tenant) == "" {
		return nil, fmt.Errorf("ebpf: pod program tenant is empty")
	}

	if len(tenant) > maxTenantIDBytes {
		return nil, fmt.Errorf(
			"ebpf: pod program tenant %q is too long: got %d bytes, maximum is %d",
			tenant,
			len(tenant),
			maxTenantIDBytes,
		)
	}

	if attach == nil {
		return nil, fmt.Errorf("ebpf: pod program attacher is nil")
	}

	handle, err := attach(ifName, tenant)
	if err != nil {
		return nil, fmt.Errorf(
			"ebpf: attach pod program to %q for tenant %q: %w",
			ifName,
			tenant,
			err,
		)
	}
	if handle == nil {
		return nil, fmt.Errorf(
			"ebpf: attach pod program to %q for tenant %q returned nil handle",
			ifName,
			tenant,
		)
	}

	return &PodProgram{
		handle: handle,
	}, nil
}
