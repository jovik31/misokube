package noderouting

import (
	"fmt"
	"net"
	"net/netip"

	"github/setera/pkg/network"
)

const (
	DefaultInterfaceName = "setera-vxlan0"
	DefaultVNI           = 100
	DefaultVXLANPort     = 4789
)

// Config configures the one node-wide Setera VXLAN interface.
type Config struct {
	InterfaceName string
	VNI           int
	Port          int
	MTU           int
}

// DefaultConfig returns the local node-routing configuration used by the
// daemon. MTU is shared with Pod veths so packets fit inside the VXLAN tunnel.
func DefaultConfig(mtu int) Config {
	return Config{
		InterfaceName: DefaultInterfaceName,
		VNI:           DefaultVNI,
		Port:          DefaultVXLANPort,
		MTU:           mtu,
	}
}

// LocalNode describes the local Setera VTEP after it has been created.
type LocalNode struct {
	InterfaceName string
	IfIndex       int
	UnderlayIP    netip.Addr
	VTEPIP        netip.Addr
	VTEPMAC       net.HardwareAddr
}

// VTEPAddress returns the first usable IPv4 address in a Node PodCIDR as a
// /32. Setera reserves this address exclusively for the node-wide VTEP.
//
// Example: 10.244.1.0/24 -> 10.244.1.1/32.
func VTEPAddress(podCIDR netip.Prefix) (netip.Prefix, error) {
	podCIDR = podCIDR.Masked()
	if !podCIDR.IsValid() || !podCIDR.Addr().Unmap().Is4() {
		return netip.Prefix{}, fmt.Errorf("noderouting: invalid IPv4 PodCIDR %s", podCIDR)
	}
	if podCIDR.Bits() > 30 {
		return netip.Prefix{}, fmt.Errorf("noderouting: PodCIDR %s is too small for VTEP and Pod addresses", podCIDR)
	}

	vtepIP := podCIDR.Addr().Unmap().Next()
	if !vtepIP.IsValid() || !podCIDR.Contains(vtepIP) {
		return netip.Prefix{}, fmt.Errorf("noderouting: cannot derive VTEP address from %s", podCIDR)
	}

	return netip.PrefixFrom(vtepIP, 32), nil
}

func (r *Routing) ensureLocalLocked(podCIDR netip.Prefix, underlayIP netip.Addr) (LocalNode, error) {
	underlayIP = underlayIP.Unmap()
	if !underlayIP.IsValid() || !underlayIP.Is4() || underlayIP.Zone() != "" || underlayIP.IsUnspecified() {
		return LocalNode{}, fmt.Errorf("noderouting: invalid local underlay IPv4 address %s", underlayIP)
	}

	vtepPrefix, err := VTEPAddress(podCIDR)
	if err != nil {
		return LocalNode{}, err
	}

	if r.local != nil {
		if r.local.VTEPIP != vtepPrefix.Addr() {
			return LocalNode{}, fmt.Errorf(
				"noderouting: local VTEP already initialized as %s, cannot switch to %s",
				r.local.VTEPIP,
				vtepPrefix.Addr(),
			)
		}
		if r.local.UnderlayIP != underlayIP {
			return LocalNode{}, fmt.Errorf(
				"noderouting: local underlay already initialized as %s, cannot switch to %s",
				r.local.UnderlayIP,
				underlayIP,
			)
		}
	}

	link, err := r.network.EnsureVXLAN(network.VXLANConfig{
		Name:       r.config.InterfaceName,
		VNI:        r.config.VNI,
		Port:       r.config.Port,
		MTU:        r.config.MTU,
		Address:    vtepPrefix,
		UnderlayIP: underlayIP,
	})
	if err != nil {
		return LocalNode{}, fmt.Errorf("noderouting: ensure local VXLAN: %w", err)
	}

	if r.setVXLANIfIndex == nil {
		return LocalNode{}, fmt.Errorf("noderouting: VXLAN ifindex publisher is nil")
	}
	if err := r.setVXLANIfIndex(link.IfIndex); err != nil {
		return LocalNode{}, fmt.Errorf("noderouting: publish VXLAN ifindex %d: %w", link.IfIndex, err)
	}

	if r.program == nil {
		program, err := r.attachNodeProgram(link.Name)
		if err != nil {
			return LocalNode{}, fmt.Errorf("noderouting: attach node datapath: %w", err)
		}
		r.program = program
	}

	local := LocalNode{
		InterfaceName: link.Name,
		IfIndex:       link.IfIndex,
		UnderlayIP:    underlayIP,
		VTEPIP:        vtepPrefix.Addr(),
		VTEPMAC:       append(net.HardwareAddr(nil), link.MAC...),
	}
	r.local = &local
	return cloneLocalNode(local), nil
}

func cloneLocalNode(node LocalNode) LocalNode {
	node.VTEPMAC = append(net.HardwareAddr(nil), node.VTEPMAC...)
	return node
}