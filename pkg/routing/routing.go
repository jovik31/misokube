// package routing contains helper functions to set up ip‑forwarding and iptables rules
// for multi‑tenant KinD clusters.  The comment section up top is a pocket playbook
// so ops engineers can see—in plain shell—what rules must exist.
//
// ─────────  EFFECTIVE IPTABLES RULESET  (recap) ─────────
// 1. Cluster‑wide MASQUERADE (every node):
//      iptables -t nat -A POSTROUTING -s 10.244.0.0/16 ! -d 10.244.0.0/16 -j MASQUERADE
// 2. Per‑worker SNAT (green / red / blue → default) now refactored into
//    _one per‑tenant chain_ so the table stays readable.  Each worker:
//      iptables -t nat -N NAT‑<TENANT>
//      iptables ‑A POSTROUTING -s <TENANT_CIDR> -j NAT‑<TENANT>
//      [inside NAT‑<TENANT>] 1‑to‑many SNAT lines to every remote default /24
// 3. Optional FORWARD helpers in per‑tenant chains if policy==DROP.
//
// The Go helpers below create / remove those per‑tenant chains and rules.
// ─────────────────────────────────────────────────────────

package routing

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/coreos/go-iptables/iptables"
	"github.com/pkg/errors"
)

// EnableIPForwarding sets net.ipv4.ip_forward=1.
func EnableIPForwarding() error {
	cmd := exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1")
	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "enable IP forwarding")
	}
	return nil
}

// AddClusterMasquerade installs the "one‑liner" masquerade rule that lets pods
// reach host‑network endpoints (API‑server, node exporters, etc.).  Safe to call
// repeatedly; uses AppendUnique semantics.
func AddClusterMasquerade() error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}
	rule := []string{"-s", "10.244.0.0/16", "!", "-d", "10.244.0.0/16", "-j", "MASQUERADE"}
	return ipt.AppendUnique("nat", "POSTROUTING", rule...)
}

// --------------------------------------------------------
// Per‑Tenant / Per‑Worker helpers
// --------------------------------------------------------

// EnsureTenantChains creates (if missing) the NAT and FILTER chains for a tenant.
//
//	tenantSlug   → short string used in chain names, e.g. "RED" or "BLUE".
//	needFilterCh → create a FW‑<TENANT> chain if FORWARD policy is DROP.
func EnsureTenantChains(tenantSlug string, needFilterCh bool) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}
	natName := fmt.Sprintf("NAT-%s", tenantSlug)
	if err := ipt.NewChain("nat", natName); err != nil && !iptablesExistsErr(err) {
		return err
	}
	if needFilterCh {
		filterName := fmt.Sprintf("FW-%s", tenantSlug)
		if err := ipt.NewChain("filter", filterName); err != nil && !iptablesExistsErr(err) {
			return err
		}
	}
	return nil
}

// InsertTenantJump adds a single jump rule from POSTROUTING into the
// per‑tenant chain so all packets sourced from tenantCIDR hit that chain.
func InsertTenantJump(tenantSlug, tenantCIDR string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}
	jumpRule := []string{"-s", tenantCIDR, "-j", fmt.Sprintf("NAT-%s", tenantSlug)}
	return ipt.AppendUnique("nat", "POSTROUTING", jumpRule...)
}

// AddSNATRule adds a line *inside* the tenant NAT chain translating traffic
// departing this worker toward a remote default /24.
func AddSNATRule(tenantSlug, remoteDefaultCIDR, nodeVTEPDev, nodeVTEPIP string) error {
	chain := fmt.Sprintf("NAT-%s", tenantSlug)
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}
	rule := []string{"-d", remoteDefaultCIDR, "-o", nodeVTEPDev, "-j", "SNAT", "--to-source", nodeVTEPIP}
	return ipt.AppendUnique("nat", chain, rule...)
}

// AddForwardAccept inserts ACCEPT inside FW‑<TENANT> so first packets are allowed.
func AddForwardAccept(tenantSlug, remoteDefaultCIDR string) error {
	chain := fmt.Sprintf("FW-%s", tenantSlug)
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}
	rule := []string{"-d", remoteDefaultCIDR, "-j", "ACCEPT"}
	if err := ipt.AppendUnique("filter", chain, rule...); err != nil {
		return err
	}
	return nil
}

// --------------------------------------------------------
// Legacy helpers kept for compatibility
// --------------------------------------------------------

// AllowBridgeForward inserts "-i <bridge> -j ACCEPT" in filter/FORWARD.
func AllowBridgeForward(bridgeInterface string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		log.Printf("Error creating iptables: %s", err.Error())
		return err
	}
	return ipt.AppendUnique("filter", "FORWARD", "-i", bridgeInterface, "-j", "ACCEPT")
}

// AllowForwardingTenant keeps the old behaviour (symmetric ACCEPT) for a CIDR.
func AllowForwardingTenant(tenantCIDR string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}
	if err := ipt.AppendUnique("filter", "FORWARD", "-s", tenantCIDR, "-j", "ACCEPT"); err != nil {
		return err
	}
	return ipt.AppendUnique("filter", "FORWARD", "-d", tenantCIDR, "-j", "ACCEPT")
}

// BlockTenant2TenantTraffic adds symmetric DROP at top of FORWARD.
func BlockTenant2TenantTraffic(t1CIDR, t2CIDR string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}
	if err := ipt.InsertUnique("filter", "FORWARD", 1, "-s", t1CIDR, "-d", t2CIDR, "-j", "DROP"); err != nil {
		return err
	}
	return ipt.InsertUnique("filter", "FORWARD", 1, "-s", t2CIDR, "-d", t1CIDR, "-j", "DROP")
}

// --------------------------------------------------------
// helper
// --------------------------------------------------------
func iptablesExistsErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "exists")
}
