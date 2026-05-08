package policy

import "context"

type TenantPolicyManager interface {

	// chain lifecycle
	EnsureTenantChains(tenant string) error
	DeleteTenantChains(tenant, brIf, vxIf string) error

	// Interface-based isolation (no CIDRs; subnet expansion requires no rule updates)
	// Creates jumps from FORWARD to FW-<tenant> by ingress iface; inside FW-<tenant>
	// allows to the tenant's own ifaces and optionally allows an uplink iface;
	// then appends a default DROP.
	EnsureTenantIsolation(tenant, brIf, vxIf string, extraAllowedEgressIfaces ...string) error

	// Default-tenant bypass: insert early FORWARD accepts so traffic from/to
	// the default tenant's ifaces is allowed regardless of destination/source.
	EnsureDefaultTenant(brDefault, vxDefault string) error

	// Generic low-level delete (handy for explicit rule teardown in tests)
	DeleteRule(table, chain string, rulespec ...string) error
}

var DefaultTenantPolicyManager TenantPolicyManager

func RegisterTenantPolicyManager(mgr TenantPolicyManager) {

	if mgr == nil {
		panic("TenantPolicy manager is nil")
	}
	DefaultTenantPolicyManager = mgr
}

func Manager() TenantPolicyManager {
	return DefaultTenantPolicyManager
}

type NodeNetworkManager interface {

	// enable L3 forwarding on the host
	EnsureIPForwarding() error

	// ensure bridge netfilter is enabled on the host
	EnsureBridgeNetfilter() error

	// ensure default forward policy is DROP
	EnsureForwardPolicyDrop() error

	// ensure NAT for pod/tenant egress traffic
	EnsureClusterMasquerade(clusterCIDR string) error
}

var DefaultNodeNetworkManager NodeNetworkManager

func RegisterNodeNetworkManager(mgr NodeNetworkManager) {
	if mgr == nil {
		panic("NodeNetworkManager is nil")
	}
	DefaultNodeNetworkManager = mgr
}

func NodeManager() NodeNetworkManager {
	return DefaultNodeNetworkManager
}

type PodPolicyManager interface {
	EnsurePodProgram(ctx context.Context, tenantID string, podName string, ifName string) error
	UpdatePodProgram(ctx context.Context, tenantID string, podName string, ifName string) error
	RemovePodProgram(ctx context.Context, tenantID string, podName string) error
}

var DefaultPodPolicyManager PodPolicyManager

func RegisterPodPolicyManager(mgr PodPolicyManager) {
	if mgr == nil {
		panic("PodPolicyManager is nil")
	}
	DefaultPodPolicyManager = mgr
}

func PodManager() PodPolicyManager {
	return DefaultPodPolicyManager
}
