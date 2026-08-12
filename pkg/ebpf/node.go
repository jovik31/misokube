package ebpf

import (
	"fmt"
	"strings"
	"sync"

	"github/setera/pkg/ebpf/loader"
)

// NodeProgram is the Setera TC program attached to a node-level interface.
//
// The node program has no tenant identity.
// Pod programs enforce tenant isolation.
type NodeProgram struct {
	closeOnce sync.Once
	closeErr  error

	handle nodeProgramHandle
}

// AttachNodeProgram loads and attaches the Setera node TC program.
//
// Linux routing and netfilter process traffic after VXLAN decapsulation.
func AttachNodeProgram(
	ifName string,
) (*NodeProgram, error) {
	return attachNodeProgram(
		ifName,
		func(
			ifName string,
		) (nodeProgramHandle, error) {
			return loader.NewNodeRouter(ifName)
		},
	)
}

// Close detaches the node TC program and releases its eBPF resources.
//
// It is safe to call Close more than once.
func (p *NodeProgram) Close() error {
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

type nodeProgramHandle interface {
	Close() error
}

type nodeProgramAttacher func(
	ifName string,
) (nodeProgramHandle, error)

func attachNodeProgram(
	ifName string,
	attach nodeProgramAttacher,
) (*NodeProgram, error) {
	if strings.TrimSpace(ifName) == "" {
		return nil, fmt.Errorf(
			"ebpf: node program interface name is empty",
		)
	}

	if attach == nil {
		return nil, fmt.Errorf(
			"ebpf: node program attacher is nil",
		)
	}

	handle, err := attach(ifName)
	if err != nil {
		return nil, fmt.Errorf(
			"ebpf: attach node program to %q: %w",
			ifName,
			err,
		)
	}

	if handle == nil {
		return nil, fmt.Errorf(
			"ebpf: attach node program to %q returned nil handle",
			ifName,
		)
	}

	return &NodeProgram{
		handle: handle,
	}, nil
}
