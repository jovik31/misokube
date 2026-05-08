package ebpfmanager

import (
	"errors"
	"net"
	"sync"

	"github/setera/pkg/network/policy"
)

type EbpfManagerImpl struct {
	RootCIDR *net.IPNet
	NodeName string

	mu sync.RWMutex

	TenantRecords map[string]*TenantRecord

	TP policy.TenantPolicyManager
	PP policy.PodPolicyManager

	nodeRouterMu    sync.Mutex
	nodeRouter      interface{ Close() error }
	nodeRouterIface string

	defaultPodIfName func(tenantID, podName string) string
}

type Deps struct {
	TenantPolicy policy.TenantPolicyManager
	PodPolicy    policy.PodPolicyManager
}

type TenantRecord struct {
	TenantID string
	State    TenantState
	Pods     map[string]*PodRecord
}

type PodRecord struct {
	PodName string
	IfName  string
}

type TenantState int

const (
	TenantStateReady TenantState = iota
	TenantStateClosing
)

var (
	ErrTenantNotFound = errors.New("tenant not found")
	ErrTenantClosing  = errors.New("tenant closing")
)
