package nmanager

import (
	"net"
	"sync"
	"time"

	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/ipam"
	"github/setera/pkg/network/subnet"

	// managers
	"github/setera/pkg/network/arp"
	"github/setera/pkg/network/fdb"
	"github/setera/pkg/network/policy"
	"github/setera/pkg/network/route"
	op "github/setera/pkg/operator"
)

// NetworkManagerImpl is the concrete implementation used internally.
// Public interfaces are declared in interface.go.
type NetworkManagerImpl struct {
	RootCIDR *net.IPNet
	NodeName string

	mu sync.RWMutex

	// Records keyed by tenant ID
	TenantRecords map[string]*TenantRecord

	// Per-tenant actors keyed by tenant ID
	TenantActors map[string]TenantActor

	// Dependencies
	Route  route.RouteManager
	ARP    arp.ARPManager
	FDB    fdb.FDBManager
	TP     policy.TenantPolicyManager
	Subnet subnet.SubnetManager

	// No factories; backend and ipam are constructed via backend.NewBackend and ipam.NewIPAM
	// add tenant actor here

	// emitter allows NM to enqueue events into the daemon operator
	emitter op.Emitter

	DefaultRoutes map[string]map[string]RemoteTenantInfra // tenantID -> nodeName -> RemoteTenantInfra
}

// Deps allows explicit injection of manager dependencies. Nil fields fall back to package defaults.
type Deps struct {
	Route  route.RouteManager
	ARP    arp.ARPManager
	FDB    fdb.FDBManager
	TP     policy.TenantPolicyManager
	Subnet subnet.SubnetManager
}

// TenantRecord holds per-tenant runtime components.
type TenantRecord struct {
	Subnet  *net.IPNet
	Backend backend.Backend
	IPAM    ipam.IPAM
	State   TenantState

	expandCh      chan struct{}
	lastExpandErr error
	lastExpandAt  time.Time
	Peers         map[string]RemoteTenantInfra
}

// TenantState indicates whether a tenant is ready to process pod ops.
type TenantState int

const (
	TenantStateReady TenantState = iota
	// TenantStateClosing indicates RemoveTenant is in progress; pod ops should be rejected.
	TenantStateClosing
)

// SetEmitter attaches an operator emitter for NM → operator communication.
func (nm *NetworkManagerImpl) SetEmitter(e op.Emitter) {
	nm.emitter = e
}

func (nm *NetworkManagerImpl) emitNodeStoreEvent(ev op.Event) {
	if nm == nil || nm.emitter == nil || nm.NodeName == "" {
		return
	}
	if ev == "" {
		ev = op.EventUpdate
	}
	nm.emitter.EnqueueWith("nm:network-manager", ev, op.ResourceRef{
		Kind:      "NodeStore",
		Namespace: "default",
		Name:      nm.NodeName,
	})
}
