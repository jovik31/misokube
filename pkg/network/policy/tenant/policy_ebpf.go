package tenant

import (
	"fmt"

	tp "github/setera/pkg/network/policy"
)

// ebpfTenantPolicyManager is a rough TenantPolicyManager implementation backed by
// the TC eBPF firewall used in eBPF_test. It focuses on wiring and lifecycle;
// detailed per-tenant rule programming is intentionally left as TODOs.

var _ tp.TenantPolicyManager = (*ebpfTenantPolicyManager)(nil)

type ebpfTenantPolicyManager struct {
}

// NewEBPFTenantPolicyManager constructs TenantPolicyManager.
func NewEBPFTenantPolicyManager() tp.TenantPolicyManager {
	return &ebpfTenantPolicyManager{}
}

func init() {
	mgr := NewEBPFTenantPolicyManager()
	tp.RegisterTenantPolicyManager(mgr)
}

// EnsureTenantChains is a no-op for the eBPF backend. The iptables backend
// used chains to structure rules; here we rely on the attached TC programs
// and BPF maps instead.
func (m *ebpfTenantPolicyManager) EnsureTenantChains(tenant string) error {
	if tenant == "" {
		return fmt.Errorf("tenant cannot be empty")
	}
	return nil
}

// EnsureTenantIsolation is intentionally a no-op in the pod-veth TC model.
// Tenant isolation is enforced by per-pod program attachments.
func (m *ebpfTenantPolicyManager) EnsureTenantIsolation(tenant, brIf, vxIf string, extraAllowedEgressIfaces ...string) error {
	if tenant == "" {
		return fmt.Errorf("tenant cannot be empty")
	}
	return nil
}

// EnsureDefaultTenant is intentionally a no-op in the pod-veth TC model.
func (m *ebpfTenantPolicyManager) EnsureDefaultTenant(brDefault, vxDefault string) error {
	_ = brDefault
	_ = vxDefault
	return nil
}

// DeleteTenantChains is intentionally a no-op in the pod-veth TC model.
func (m *ebpfTenantPolicyManager) DeleteTenantChains(tenant, brIf, vxIf string) error {
	if tenant == "" {
		return fmt.Errorf("tenant is empty")
	}
	_ = brIf
	_ = vxIf
	return nil
}

// DeleteRule is not yet implemented for the eBPF backend. The original
// iptables implementation accepted raw iptables rulespecs; mapping that to
// structured BPF rules requires additional design.
func (m *ebpfTenantPolicyManager) DeleteRule(table, chain string, rulespec ...string) error {
	return fmt.Errorf("DeleteRule is not implemented for eBPF tenant policy backend")
}
