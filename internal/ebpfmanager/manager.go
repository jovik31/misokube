package ebpfmanager

import (
	"errors"
	"net/netip"
	"sync"

	"github/setera/pkg/ebpf"
)

var (
	ErrInvalidLocalPod  = errors.New("ebpfmanager: invalid local pod")
	ErrLocalPodConflict = errors.New("ebpfmanager: local pod conflict")
)

// Manager owns Setera's node-local eBPF lifecycle state.
//
// The first implementation intentionally manages local Pods only. Kubernetes
// watches, remote Pods, and restart reconciliation are added separately so
// this package keeps one clear responsibility at each stage of the refactor.
type Manager struct {
	mu sync.Mutex

	local map[netip.Addr]localPodState
	deps  dependencies
}

// New returns a concrete Setera eBPF manager.
func New() *Manager {
	return newWithDependencies(defaultDependencies())
}

type localPodState struct {
	pod     localPodRecord
	program podProgram
}

type localPodRecord struct {
	PodUID          string
	TenantID        string
	HostVethName    string
	HostVethIfIndex int
}

type podProgram interface {
	Close() error
}

type dependencies struct {
	attachPodProgram func(ifName, tenant string) (podProgram, error)
	upsertEndpoint   func(ip netip.Addr, tenant string, ifIndex int) error
	deleteEndpoint   func(ip netip.Addr) error
}

func defaultDependencies() dependencies {
	return dependencies{
		attachPodProgram: func(ifName, tenant string) (podProgram, error) {
			return ebpf.AttachPodProgram(ifName, tenant)
		},
		upsertEndpoint: ebpf.UpsertPodEndpoint,
		deleteEndpoint: ebpf.DeletePodEndpoint,
	}
}

func newWithDependencies(deps dependencies) *Manager {
	return &Manager{
		local: make(map[netip.Addr]localPodState),
		deps:  deps,
	}
}
