package iptable

type IPtableManager interface {
	AddClusterMasquerade(clusterCIDR string) error

	// chain lifecycle
	EnsureTenantChains(tenant string) error
	DeleteTenantChains(tenant string) error

	// Interface-based isolation (no CIDRs; subnet expansion requires no rule updates)
	// Creates jumps from FORWARD to FW-<tenant> by ingress iface; inside FW-<tenant>
	// allows to the tenant's own ifaces and optionally allows an uplink iface;
	// then appends a default DROP.
	EnsureTenantIsolationByIface(tenant, brIf, vxIf string, extraAllowedEgressIfaces ...string) error

	// Default-tenant bypass: insert early FORWARD accepts so traffic from/to
	// the default tenant's ifaces is allowed regardless of destination/source.
	EnsureDefaultTenantPassByIface(brDefault, vxDefault string) error

	// Generic low-level delete (handy for explicit rule teardown in tests)
	DeleteRule(table, chain string, rulespec ...string) error
}

var DefaultIPtableManager IPtableManager

func RegisterIPtableManager(mgr IPtableManager) {

	if mgr == nil {
		panic("IPtableManager is nil")
	}
	DefaultIPtableManager = mgr
}

func IPTableManager() IPtableManager {
	return DefaultIPtableManager
}
