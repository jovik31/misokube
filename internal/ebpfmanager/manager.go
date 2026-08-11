package ebpfmanager

import (
	"errors"
	"net/netip"
	"sync"

	"github/setera/pkg/ebpf"
)

var (
	ErrInvalidLocalPod   = errors.New("ebpfmanager: invalid local pod")
	ErrLocalPodConflict  = errors.New("ebpfmanager: local pod conflict")
	ErrInvalidRemotePod  = errors.New("ebpfmanager: invalid remote pod")
	ErrRemotePodConflict = errors.New("ebpfmanager: remote pod conflict")
)

// Manager owns Setera's node-local eBPF lifecycle state.
//
// It tracks local and remote Pod ownership for the shared tc_podIDs keyspace.
// Kubernetes watches and routing remain outside this package.
type Manager struct {
	mu sync.Mutex

	local  map[netip.Addr]localPodState
	remote map[netip.Addr]remotePodRecord
	deps   dependencies
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

type remotePodRecord struct {
	PodUID   string
	TenantID string
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
		local:  make(map[netip.Addr]localPodState),
		remote: make(map[netip.Addr]remotePodRecord),
		deps:   deps,
	}
}
