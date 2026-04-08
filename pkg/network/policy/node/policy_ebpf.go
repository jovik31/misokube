package node

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/coreos/go-iptables/iptables"

	tp "github/setera/pkg/network/policy"
)

var _ tp.NodeNetworkManager = (*nodePolicyManager)(nil)

type nodePolicyManager struct {
	ipt *iptables.IPTables
}

func NewNodeNetworkManagerWithHandle(handle *iptables.IPTables) tp.NodeNetworkManager {
	return &nodePolicyManager{ipt: handle}
}

func NewNodeNetworkManager() (tp.NodeNetworkManager, error) {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return nil, err
	}
	return NewNodeNetworkManagerWithHandle(ipt), nil
}

func init() {
	mgr, err := NewNodeNetworkManager()
	if err != nil {
		panic(err)
	}
	tp.RegisterNodeNetworkManager(mgr)
}

func (n *nodePolicyManager) EnsureIPForwarding() error {
	if err := writeSysctl("net.ipv4.ip_forward", "1", false); err != nil {
		return err
	}
	// IPv6 forwarding may not be available; ignore missing sysctls.
	return writeSysctl("net.ipv6.conf.all.forwarding", "1", true)
}

func (n *nodePolicyManager) EnsureBridgeNetfilter() error {
	_ = exec.Command("modprobe", "br_netfilter").Run()
	if err := writeSysctl("net.bridge.bridge-nf-call-iptables", "1", true); err != nil {
		return err
	}
	return writeSysctl("net.bridge.bridge-nf-call-ip6tables", "1", true)
}

func (n *nodePolicyManager) EnsureForwardPolicyDrop() error {
	// No-op when using eBPF-based tenant isolation: we rely on
	// the TC program to enforce forwarding policy instead of
	// using a default DROP on the iptables FORWARD chain.
	return nil
}

func (n *nodePolicyManager) EnsureClusterMasquerade(clusterCIDR string) error {
	if clusterCIDR == "" {
		return fmt.Errorf("clusterCIDR cannot be empty")
	}
	if n.ipt == nil {
		return errors.New("iptables handle not initialized")
	}
	return n.ipt.AppendUnique("nat", "POSTROUTING",
		"-s", clusterCIDR,
		"!", "-d", clusterCIDR,
		"-j", "MASQUERADE",
	)
}

func writeSysctl(key, value string, ignoreMissing bool) error {
	cmd := exec.Command("sysctl", "-w", fmt.Sprintf("%s=%s", key, value))
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if ignoreMissing && isIgnorableSysctlError(outStr) {
			return nil
		}
		return fmt.Errorf("sysctl -w %s=%s failed: %w (%s)", key, value, err, outStr)
	}
	return nil
}

func isIgnorableSysctlError(msg string) bool {
	if msg == "" {
		return false
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "no such file") ||
		strings.Contains(lower, "invalid argument")
}
