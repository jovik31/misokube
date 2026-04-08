package tenant

import (
	"fmt"
	"net"
	"sync"

	ebpffw "github/setera/pkg/network/policy/loader"
	tp "github/setera/pkg/network/policy"
)

// ebpfTenantPolicyManager is a rough TenantPolicyManager implementation backed by
// the TC eBPF firewall used in eBPF_test. It focuses on wiring and lifecycle;
// detailed per-tenant rule programming is intentionally left as TODOs.

var _ tp.TenantPolicyManager = (*ebpfTenantPolicyManager)(nil)

type ebpfTenantPolicyManager struct {
	mu sync.Mutex
	// firewalls keyed by interface name (bridge / vxlan / uplink).
	ifs map[string]*ebpffw.TCFirewall
}

// NewEBPFTenantPolicyManager constructs TenantPolicyManager.
func NewEBPFTenantPolicyManager() tp.TenantPolicyManager {
	return &ebpfTenantPolicyManager{
		ifs: make(map[string]*ebpffw.TCFirewall),
	}
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

// EnsureTenantIsolation attaches TC firewall programs to the tenant's
// bridge/vxlan interfaces (if not already attached) and sets a conservative
// default DROP policy. Fine-grained per-tenant allow rules should be added
// later via BPF rule programming (see eBPF_test/pkg/firewall).
func (m *ebpfTenantPolicyManager) EnsureTenantIsolation(tenant, brIf, vxIf string, extraAllowedEgressIfaces ...string) error {
	if tenant == "" {
		return fmt.Errorf("tenant cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	fw, ok := m.ifs[brIf]
	if !ok {
		var err error
		fw, err = ebpffw.NewTCFirewall(brIf)
		if err != nil {
			return fmt.Errorf("attach tc firewall to %s: %w", brIf, err)
		}
		if err := fw.SetDefaultAction(ebpffw.ActionDrop, true); err != nil {
			_ = fw.Close()
			return fmt.Errorf("set default drop on %s: %w", brIf, err)
		}
		m.ifs[brIf] = fw
	}
	// Look up the interface index and write the tenant binding into the
	// tc_iface_tenant map.
	iface_br, err := net.InterfaceByName(brIf)
	if err != nil {
		return fmt.Errorf("lookup interface %s: %w", brIf, err)
	}
	if err := fw.AddInterfaceTenantBinding(iface_br.Index, tenant); err != nil {
		return fmt.Errorf("set tenant binding for %s: %w", brIf, err)
	}

	iface_vx, err := net.InterfaceByName(vxIf)
	if err != nil {
		return fmt.Errorf("lookup interface %s: %w", vxIf, err)
	}
	if err := fw.AddInterfaceTenantBinding(iface_vx.Index, tenant); err != nil {
		return fmt.Errorf("set tenant binding for %s: %w", vxIf, err)
	}
	return nil
}

// EnsureDefaultTenant ensures that the default tenant's bridge/vxlan
// interfaces, if firewalled, use a default ALLOW policy. This roughly
// corresponds to the iptables backend inserting early ACCEPT rules for the
// default tenant.
func (m *ebpfTenantPolicyManager) EnsureDefaultTenant(brDefault, vxDefault string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ensureAllow := func(ifName string) error {
		if ifName == "" {
			return nil
		}
		fw, ok := m.ifs[ifName]
		if !ok {
			var err error
			fw, err = ebpffw.NewTCFirewall(ifName)
			if err != nil {
				return fmt.Errorf("attach tc firewall to %s: %w", ifName, err)
			}
			m.ifs[ifName] = fw
		}
		if err := fw.SetDefaultAction(ebpffw.ActionAllow, true); err != nil {
			return fmt.Errorf("set default allow on %s: %w", ifName, err)
		}
		// Also record the interface→tenant binding for the default tenant so
		// the BPF program can allow default↔other-tenant traffic while still
		// enforcing isolation between non-default tenants.
		iface, err := net.InterfaceByName(ifName)
		if err != nil {
			return fmt.Errorf("lookup interface %s: %w", ifName, err)
		}
		if err := fw.AddInterfaceTenantBinding(iface.Index, "default"); err != nil {
			return fmt.Errorf("set tenant binding for %s: %w", ifName, err)
		}
		return nil
	}

	// Only manage the bridge interface here; we keep the TC program
	// attached on bridges, not vxlan devices.
	if err := ensureAllow(brDefault); err != nil {
		return err
	}

	return nil
}

// DeleteTenantChains detaches and cleans up TC firewalls for the tenant's
// bridge/vxlan interfaces, if they were previously attached by this manager.
func (m *ebpfTenantPolicyManager) DeleteTenantChains(tenant, brIf, vxIf string) error {
	if tenant == "" {
		return fmt.Errorf("tenant is empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ifName := range []string{brIf, vxIf} {
		if ifName == "" {
			continue
		}
		if fw, ok := m.ifs[ifName]; ok && fw != nil {
			_ = fw.Close()
			delete(m.ifs, ifName)
		}
	}

	return nil
}

// DeleteRule is not yet implemented for the eBPF backend. The original
// iptables implementation accepted raw iptables rulespecs; mapping that to
// structured BPF rules requires additional design.
func (m *ebpfTenantPolicyManager) DeleteRule(table, chain string, rulespec ...string) error {
	return fmt.Errorf("DeleteRule is not implemented for eBPF tenant policy backend")
}

