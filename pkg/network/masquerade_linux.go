//go:build linux

package network

import (
	"fmt"
	"net/netip"

	"github.com/coreos/go-iptables/iptables"
)

type iptablesAppender interface {
	AppendUnique(table, chain string, rulespec ...string) error
}

var newIPv4IPTables = func() (iptablesAppender, error) {
	return iptables.NewWithProtocol(iptables.ProtocolIPv4)
}

// EnsureIPv4Masquerade ensures IPv4 traffic sourced from the cluster-wide Pod
// CIDR is masqueraded when it leaves that CIDR.
//
// The argument must be the cluster-wide Pod CIDR, not a node-local PodCIDR.
// Using a node-local PodCIDR would incorrectly SNAT cross-node Pod traffic.
func (n *Linux) EnsureIPv4Masquerade(clusterPodCIDR netip.Prefix) error {
	if !clusterPodCIDR.IsValid() || !clusterPodCIDR.Addr().Is4() {
		return fmt.Errorf(
			"ensure IPv4 masquerade: invalid cluster Pod CIDR %s",
			clusterPodCIDR,
		)
	}

	clusterPodCIDR = clusterPodCIDR.Masked()

	ipt, err := newIPv4IPTables()
	if err != nil {
		return fmt.Errorf("ensure IPv4 masquerade: create iptables handle: %w", err)
	}

	cidr := clusterPodCIDR.String()
	if err := ipt.AppendUnique(
		"nat",
		"POSTROUTING",
		"-s", cidr,
		"!", "-d", cidr,
		"-j", "MASQUERADE",
	); err != nil {
		return fmt.Errorf(
			"ensure IPv4 masquerade for %s: %w",
			cidr,
			err,
		)
	}

	return nil
}
