package netiptable

import (
	"fmt"
	"github.com/coreos/go-iptables/iptables"
	"github/setera/pkg/network/iptable"
	"strings"
)

var _ iptable.IPtableManager = (*netlinkIPtableManager)(nil)

type netlinkIPtableManager struct {
	nl NetlinkIPTableHandle
}

func NewIPtableManager(handle NetlinkIPTableHandle) iptable.IPtableManager {

	return &netlinkIPtableManager{
		nl: handle,
	}
}

func init() {

	mgr, err := NewV4()
	if err != nil {
		panic(err)
	}
	iptable.RegisterIPtableManager(mgr)
}

func NewV4() (iptable.IPtableManager, error) {

	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return nil, err
	}
	return NewIPtableManager(rNetlinkIPTableHandle{ipt: ipt}), nil
}
func (manager *netlinkIPtableManager) AddClusterMasquerade(clusterCIDR string) error {

	if clusterCIDR == "" {
		return fmt.Errorf("clusterCIDR cannot be empty")
	}

	return manager.nl.AppendUnique(
		"nat", "POSTROUTING",
		"-s", clusterCIDR,
		"!", "-d", clusterCIDR,
		"-j", "MASQUERADE",
	)

}

func (manager *netlinkIPtableManager) EnsureTenantChains(tenant string) error {

	if tenant == "" {
		return fmt.Errorf("tenant cannot be empty")
	}

	fw := fwChain(tenant)
	_ = manager.nl.NewChain("filter", fw)
	return manager.nl.ClearChain("filter", fw)

}

func (manager *netlinkIPtableManager) DeleteTenantChains(tenant string) error {
	if tenant == "" {
		return fmt.Errorf("tenant is empty")
	}
	fw := fwChain(tenant)
	if err := manager.nl.ClearChain("filter", fw); err != nil {
		return err
	}
	return manager.nl.DeleteChain("filter", fw)
}

func (manager *netlinkIPtableManager) EnsureTenantIsolationByIface(tenant, brIf, vxIf string, extraAllowedEgressIfaces ...string) error {
	if err := manager.EnsureTenantChains(tenant); err != nil {
		return err
	}
	fw := fwChain(tenant)

	// Jumps by ingress iface
	if brIf != "" {
		if err := manager.nl.AppendUnique("filter", "FORWARD", "-i", brIf, "-j", fw); err != nil {
			return err
		}
	}
	if vxIf != "" {
		if err := manager.nl.AppendUnique("filter", "FORWARD", "-i", vxIf, "-j", fw); err != nil {
			return err
		}
	}

	// Intra-tenant accepts
	if brIf != "" {
		if err := manager.nl.AppendUnique("filter", fw, "-o", brIf, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if vxIf != "" {
		if err := manager.nl.AppendUnique("filter", fw, "-o", vxIf, "-j", "ACCEPT"); err != nil {
			return err
		}
	}

	// Optional: allow egress to uplinks/external ifaces (e.g., eth0)
	for _, ifc := range extraAllowedEgressIfaces {
		if ifc == "" {
			continue
		}
		if err := manager.nl.AppendUnique("filter", fw, "-o", ifc, "-j", "ACCEPT"); err != nil {
			return err
		}
	}

	// Default drop (comment makes idempotency deterministic with AppendUnique)
	return manager.nl.AppendUnique("filter", fw,
		"-m", "comment", "--comment", "tenant:"+tenant+":default-drop", "-j", "DROP")

}

func (manager *netlinkIPtableManager) EnsureDefaultTenantPassByIface(brDefault, vxDefault string) error {
	insertTop := func(spec ...string) error {
		// position 1 == top; InsertUnique prevents duplicates
		return manager.nl.InsertUnique("filter", "FORWARD", 1, spec...)
	}

	// Insert in reverse of desired final order
	if vxDefault != "" {
		if err := insertTop("-o", vxDefault, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if brDefault != "" {
		if err := insertTop("-o", brDefault, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if vxDefault != "" {
		if err := insertTop("-i", vxDefault, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if brDefault != "" {
		if err := insertTop("-i", brDefault, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	return nil
}

func (manager *netlinkIPtableManager) DeleteRule(table, chain string, rulespec ...string) error {
	if table == "" || chain == "" {
		return fmt.Errorf("table/chain required")
	}
	return manager.nl.Delete(table, chain, rulespec...)
}

func fwChain(tenant string) string {
	return chainName("FW-", tenant)
}

const maxChainLen = 29

// chainName joins prefix+tenant, maps invalid runes to '-', and truncates to maxChainLen.
func chainName(prefix, tenant string) string {
	if tenant == "" {
		return strings.TrimSuffix(prefix, "-")
	}
	raw := prefix + tenant
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := b.String()
	if len(name) > maxChainLen {
		name = name[:maxChainLen]
	}
	return name
}
