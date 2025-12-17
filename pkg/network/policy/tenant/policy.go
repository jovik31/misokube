package tenant

import (
	"fmt"
	tp "github/setera/pkg/network/policy"
	"strings"

	"github.com/coreos/go-iptables/iptables"
)

const (
	privateAnchorChain   = "SETERA-PRIV-FORWARD"
	privateAnchorComment = "setera:private-anchor"
)

var _ tp.TenantPolicyManager = (*netlinkTenantPolicyManager)(nil)

type netlinkTenantPolicyManager struct {
	nl NetlinkTenantPolicyHandle
}

func NewTenantPolicyManager(handle NetlinkTenantPolicyHandle) tp.TenantPolicyManager {

	return &netlinkTenantPolicyManager{
		nl: handle,
	}
}

func init() {

	mgr, err := NewV4()
	if err != nil {
		panic(err)
	}
	tp.RegisterTenantPolicyManager(mgr)
}

func NewV4() (tp.TenantPolicyManager, error) {

	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return nil, err
	}
	return NewTenantPolicyManager(rNetlinkTenantPolicyHandle{ipt: ipt}), nil
}
func (manager *netlinkTenantPolicyManager) AddClusterMasquerade(clusterCIDR string) error {

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

func (manager *netlinkTenantPolicyManager) EnsureTenantChains(tenant string) error {

	if tenant == "" {
		return fmt.Errorf("tenant cannot be empty")
	}

	fw := fwChain(tenant)
	_ = manager.nl.NewChain("filter", fw)
	return manager.nl.ClearChain("filter", fw)

}

func (manager *netlinkTenantPolicyManager) DeleteTenantChains(tenant, brIf, vxIf string) error {
	if tenant == "" {
		return fmt.Errorf("tenant is empty")
	}
	fw := fwChain(tenant)
	// Remove ingress jumps from the private anchor if present
	if err := manager.removeFromPrivateAnchor(tenant, brIf, "bridge"); err != nil {
		return err
	}
	if err := manager.removeFromPrivateAnchor(tenant, vxIf, "vxlan"); err != nil {
		return err
	}
	if err := manager.nl.ClearChain("filter", fw); err != nil {
		return err
	}
	return manager.nl.DeleteChain("filter", fw)
}

func (manager *netlinkTenantPolicyManager) EnsureTenantIsolation(tenant, brIf, vxIf string, extraAllowedEgressIfaces ...string) error {
	if err := manager.EnsureTenantChains(tenant); err != nil {
		return err
	}
	fw := fwChain(tenant)

	if err := manager.ensurePrivateAnchor(); err != nil {
		return err
	}

	insertPrivateJump := func(iface, commentSuffix string) error {
		if iface == "" {
			return nil
		}
		return manager.nl.AppendUnique(
			"filter", privateAnchorChain,
			"-i", iface,
			"-m", "comment", "--comment", "tenant:"+tenant+":ingress:"+commentSuffix,
			"-j", fw,
		)
	}

	// Jumps by ingress iface
	if err := insertPrivateJump(vxIf, "vxlan"); err != nil {
		return err
	}
	if err := insertPrivateJump(brIf, "bridge"); err != nil {
		return err
	}

	// Intra-tenant accepts
	if brIf != "" {
		if err := manager.nl.AppendUnique(
			"filter", fw,
			"-o", brIf,
			"-m", "comment", "--comment", "tenant:"+tenant+":egress:bridge",
			"-j", "ACCEPT",
		); err != nil {
			return err
		}
	}
	if vxIf != "" {
		if err := manager.nl.AppendUnique(
			"filter", fw,
			"-o", vxIf,
			"-m", "comment", "--comment", "tenant:"+tenant+":egress:vxlan",
			"-j", "ACCEPT",
		); err != nil {
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

func (manager *netlinkTenantPolicyManager) EnsureDefaultTenant(brDefault, vxDefault string) error {
	insertTop := func(comment string, spec ...string) error {
		if comment != "" {
			spec = append([]string{"-m", "comment", "--comment", comment}, spec...)
		}
		// position 1 == top; InsertUnique prevents duplicates
		return manager.nl.InsertUnique("filter", "FORWARD", 1, spec...)
	}

	// Insert in reverse of desired final order
	if vxDefault != "" {
		if err := insertTop("default-tenant-egress:vxlan", "-o", vxDefault, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if brDefault != "" {
		if err := insertTop("default-tenant-egress:bridge", "-o", brDefault, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if vxDefault != "" {
		if err := insertTop("default-tenant-ingress:vxlan", "-i", vxDefault, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if brDefault != "" {
		if err := insertTop("default-tenant-ingress:bridge", "-i", brDefault, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	return manager.ensurePrivateAnchor()
}

func (manager *netlinkTenantPolicyManager) DeleteRule(table, chain string, rulespec ...string) error {
	if table == "" || chain == "" {
		return fmt.Errorf("table/chain required")
	}
	return manager.nl.Delete(table, chain, rulespec...)
}

func (manager *netlinkTenantPolicyManager) ensurePrivateAnchor() error {
	if err := manager.ensureChainExists(privateAnchorChain); err != nil {
		return err
	}
	spec := []string{
		"-m", "comment", "--comment", privateAnchorComment,
		"-j", privateAnchorChain,
	}
	_ = manager.nl.Delete("filter", "FORWARD", spec...)
	pos, err := manager.afterDefaultBlockPosition()
	if err != nil {
		return err
	}
	return manager.nl.InsertUnique("filter", "FORWARD", pos, spec...)
}

func (manager *netlinkTenantPolicyManager) removeFromPrivateAnchor(tenant, iface, kind string) error {
	if iface == "" {
		return nil
	}
	return manager.nl.Delete("filter", privateAnchorChain,
		"-i", iface,
		"-m", "comment", "--comment", "tenant:"+tenant+":ingress:"+kind,
		"-j", fwChain(tenant))
}

func (manager *netlinkTenantPolicyManager) afterDefaultBlockPosition() (int, error) {
	rules, err := manager.nl.List("filter", "FORWARD")
	if err != nil {
		return 1, err
	}
	lastDefault := 0
	firstKube := 0
	for idx, rule := range rules {
		if strings.Contains(rule, "default-tenant-") {
			lastDefault = idx + 1
			continue
		}
		if firstKube == 0 && (strings.Contains(rule, "KUBE-") || strings.Contains(rule, "kubernetes ")) {
			firstKube = idx + 1
		}
	}
	if firstKube > 0 {
		return firstKube, nil
	}
	if lastDefault > 0 {
		return lastDefault + 1, nil
	}
	return 1, nil
}

func (manager *netlinkTenantPolicyManager) ensureChainExists(chain string) error {
	if chain == "" {
		return fmt.Errorf("chain name required")
	}
	if err := manager.nl.NewChain("filter", chain); err != nil {
		if !isChainExistsErr(err) {
			return err
		}
	}
	return nil
}

func isChainExistsErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Chain already exists")
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
