package nmanager

import (
	"net"
	"sync"

	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/ipam"
	"github/setera/pkg/network/subnet"

	// managers
	"github/setera/pkg/network/arp"
	"github/setera/pkg/network/fdb"
	"github/setera/pkg/network/iptable"
	"github/setera/pkg/network/route"
)

// NetworkManagerImpl is the concrete implementation used internally.
// Public interfaces are declared in interface.go.
type NetworkManagerImpl struct {
	RootCIDR *net.IPNet
	NodeName string

	mu sync.RWMutex

	// Records keyed by tenant ID
	TenantRecords map[string]*TenantRecord

	// Dependencies
	Route    route.RouteManager
	ARP      arp.ARPManager
	FDB      fdb.FDBManager
	IPTables iptable.IPtableManager
	Subnet   subnet.SubnetManager

	// No factories; backend and ipam are constructed via backend.NewBackend and ipam.NewIPAM
}

// Deps allows explicit injection of manager dependencies. Nil fields fall back to package defaults.
type Deps struct {
	Route    route.RouteManager
	ARP      arp.ARPManager
	FDB      fdb.FDBManager
	IPTables iptable.IPtableManager
	Subnet   subnet.SubnetManager
}

// TenantRecord holds per-tenant runtime components.
type TenantRecord struct {
	Subnet  *net.IPNet
	Backend backend.Backend
	IPAM    ipam.IPAM
}
