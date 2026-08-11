//go:build linux

package network

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"

	"github.com/vishvananda/netlink"
)

const maxVXLANVNI = 1<<24 - 1

var (
	linkAdd     = netlink.LinkAdd
	linkListAll = netlink.LinkList
)

// VXLANConfig describes one node-wide VXLAN interface.
type VXLANConfig struct {
	Name       string
	VNI        int
	Port       int
	MTU        int
	Address    netip.Prefix
	UnderlayIP netip.Addr
}

// VXLANLink identifies an ensured VXLAN interface.
type VXLANLink struct {
	Name    string
	IfIndex int
	MAC     net.HardwareAddr
}

// EnsureVXLAN creates the VXLAN interface when it does not exist and ensures
// its MTU, /32 VTEP address and link state.
//
// An existing interface is reused only when its VXLAN VNI and UDP port match
// the requested configuration. This makes daemon restart recovery idempotent
// without silently adopting an unrelated interface with the same name.
func (n *Linux) EnsureVXLAN(config VXLANConfig) (VXLANLink, error) {
	if err := validateVXLANConfig(config); err != nil {
		return VXLANLink{}, err
	}

	underlayLink, err := findLinkByIPv4(config.UnderlayIP)
	if err != nil {
		return VXLANLink{}, fmt.Errorf("resolve VXLAN underlay %s: %w", config.UnderlayIP, err)
	}
	underlayIfIndex := underlayLink.Attrs().Index
	if underlayIfIndex <= 0 {
		return VXLANLink{}, fmt.Errorf("network: VXLAN underlay %s has invalid ifindex", config.UnderlayIP)
	}

	link, err := linkByName(config.Name)
	if err != nil {
		candidate := &netlink.Vxlan{
			LinkAttrs: netlink.LinkAttrs{
				Name: config.Name,
				MTU:  config.MTU,
			},
			VxlanId:      config.VNI,
			VtepDevIndex: underlayIfIndex,
			SrcAddr:      addrToNetIP(config.UnderlayIP),
			Port:         config.Port,
			Learning:     false,
		}

		if addErr := linkAdd(candidate); addErr != nil && !errors.Is(addErr, syscall.EEXIST) {
			return VXLANLink{}, fmt.Errorf("create VXLAN %q: %w", config.Name, addErr)
		}

		link, err = linkByName(config.Name)
		if err != nil {
			return VXLANLink{}, fmt.Errorf("lookup VXLAN %q after create: %w", config.Name, err)
		}
	}

	vxlan, ok := link.(*netlink.Vxlan)
	if !ok {
		return VXLANLink{}, fmt.Errorf("network: interface %q exists but is %T, not VXLAN", config.Name, link)
	}
	if vxlan.VxlanId != config.VNI {
		return VXLANLink{}, fmt.Errorf(
			"network: VXLAN %q has VNI %d, want %d",
			config.Name,
			vxlan.VxlanId,
			config.VNI,
		)
	}
	if vxlan.Port != config.Port {
		return VXLANLink{}, fmt.Errorf(
			"network: VXLAN %q has UDP port %d, want %d",
			config.Name,
			vxlan.Port,
			config.Port,
		)
	}

	if vxlan.VtepDevIndex != underlayIfIndex {
		return VXLANLink{}, fmt.Errorf(
			"network: VXLAN %q uses underlay ifindex %d, want %d for %s",
			config.Name,
			vxlan.VtepDevIndex,
			underlayIfIndex,
			config.UnderlayIP,
		)
	}
	if vxlan.SrcAddr == nil || !vxlan.SrcAddr.Equal(addrToNetIP(config.UnderlayIP)) {
		return VXLANLink{}, fmt.Errorf(
			"network: VXLAN %q source address %v, want %s",
			config.Name,
			vxlan.SrcAddr,
			config.UnderlayIP,
		)
	}

	if err := linkSetMTU(link, config.MTU); err != nil {
		return VXLANLink{}, fmt.Errorf("set VXLAN %q MTU: %w", config.Name, err)
	}

	if err := addrReplace(link, &netlink.Addr{IPNet: prefixToIPNet(config.Address)}); err != nil {
		return VXLANLink{}, fmt.Errorf("set VXLAN %q address %s: %w", config.Name, config.Address, err)
	}

	if err := linkSetUp(link); err != nil {
		return VXLANLink{}, fmt.Errorf("bring VXLAN %q up: %w", config.Name, err)
	}

	attrs := link.Attrs()
	if attrs == nil || attrs.Index <= 0 {
		return VXLANLink{}, fmt.Errorf("network: VXLAN %q has invalid ifindex", config.Name)
	}
	if len(attrs.HardwareAddr) == 0 {
		return VXLANLink{}, fmt.Errorf("network: VXLAN %q has no MAC address", config.Name)
	}

	return VXLANLink{
		Name:    attrs.Name,
		IfIndex: attrs.Index,
		MAC:     append(net.HardwareAddr(nil), attrs.HardwareAddr...),
	}, nil
}

func validateVXLANConfig(config VXLANConfig) error {
	if err := validateInterfaceName(config.Name); err != nil {
		return err
	}
	if config.VNI <= 0 || config.VNI > maxVXLANVNI {
		return fmt.Errorf("network: invalid VXLAN VNI %d", config.VNI)
	}
	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("network: invalid VXLAN UDP port %d", config.Port)
	}
	if config.MTU <= 0 {
		return fmt.Errorf("network: invalid VXLAN MTU %d", config.MTU)
	}

	address := config.Address
	if !address.IsValid() || !address.Addr().Unmap().Is4() || address.Addr().Zone() != "" {
		return fmt.Errorf("network: invalid IPv4 VXLAN address %s", address)
	}
	if address.Bits() != 32 {
		return fmt.Errorf("network: VXLAN VTEP address must be /32, got %s", address)
	}

	underlay := config.UnderlayIP.Unmap()
	if !underlay.IsValid() || !underlay.Is4() || underlay.Zone() != "" || underlay.IsUnspecified() {
		return fmt.Errorf("network: invalid IPv4 VXLAN underlay address %s", config.UnderlayIP)
	}

	return nil
}

func findLinkByIPv4(ip netip.Addr) (netlink.Link, error) {
	ip = ip.Unmap()
	links, err := linkListAll()
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}

	for _, link := range links {
		addresses, err := addrList(link, netlink.FAMILY_V4)
		if err != nil {
			return nil, fmt.Errorf("list IPv4 addresses for %q: %w", link.Attrs().Name, err)
		}
		for _, address := range addresses {
			if address.IP == nil {
				continue
			}
			got, ok := netip.AddrFromSlice(address.IP)
			if ok && got.Unmap() == ip {
				return link, nil
			}
		}
	}

	return nil, fmt.Errorf("no local interface owns IPv4 address %s", ip)
}
