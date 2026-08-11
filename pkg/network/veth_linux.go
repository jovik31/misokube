//go:build linux

package network

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"syscall"

	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

const maxInterfaceNameLength = 15

var (
	getNS = ns.GetNS

	setupVethPair = func(ifName string, mtu int, hostNS ns.NetNS) (string, string, error) {
		hostVeth, containerVeth, err := ip.SetupVeth(ifName, mtu, "", hostNS)
		if err != nil {
			return "", "", err
		}
		return hostVeth.Name, containerVeth.Name, nil
	}

	linkByName   = netlink.LinkByName
	addrAdd      = netlink.AddrAdd
	addrReplace  = netlink.AddrReplace
	addrList     = netlink.AddrList
	linkSetMTU   = netlink.LinkSetMTU
	linkSetUp    = netlink.LinkSetUp
	linkDelete   = netlink.LinkDel
	routeReplace = netlink.RouteReplace
	neighborSet  = netlink.NeighSet
)

// SetupVeth creates a direct veth pair for a pod.
//
// The container side:
//   - is named ifName;
//   - receives podIP/32;
//   - receives an on-link route to gateway;
//   - receives a permanent gateway neighbor pointing at the host-veth MAC;
//   - receives a default route through gateway.
//
// The host side:
//   - stays in the host network namespace;
//   - has no IPv4 address;
//   - receives a host route to podIP for host reachability and restart recovery;
//   - is returned with its host-side ifindex for datapath attachment.
//
// The gateway is synthetic. It exists only as a Pod-side route/neighbor
// next-hop and is never assigned to the host veth.
func (n *Linux) SetupVeth(
	netnsPath string,
	ifName string,
	podIP netip.Addr,
	gateway netip.Addr,
	mtu int,
) (Veth, error) {
	if err := validateSetupVeth(netnsPath, ifName, podIP, gateway, mtu); err != nil {
		return Veth{}, err
	}

	podIP = podIP.Unmap()
	gateway = gateway.Unmap()

	netnsHandle, err := getNS(netnsPath)
	if err != nil {
		return Veth{}, fmt.Errorf("open netns %q: %w", netnsPath, err)
	}
	defer netnsHandle.Close()

	podNet := ipv4HostNetwork(podIP)
	gatewayNet := ipv4HostNetwork(gateway)

	var hostName string

	err = netnsHandle.Do(func(hostNS ns.NetNS) error {
		createdHostName, containerName, err := setupVethPair(ifName, mtu, hostNS)
		if err != nil {
			return fmt.Errorf("create veth pair: %w", err)
		}
		hostName = createdHostName

		containerLink, err := linkByName(containerName)
		if err != nil {
			return fmt.Errorf("lookup container veth %q: %w", containerName, err)
		}

		if err := addrAdd(containerLink, &netlink.Addr{IPNet: podNet}); err != nil && !errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("add pod address %s: %w", podIP, err)
		}

		if err := linkSetMTU(containerLink, mtu); err != nil {
			return fmt.Errorf("set container veth MTU: %w", err)
		}

		if err := linkSetUp(containerLink); err != nil {
			return fmt.Errorf("bring container veth up: %w", err)
		}

		if err := routeReplace(&netlink.Route{
			LinkIndex: containerLink.Attrs().Index,
			Dst:       gatewayNet,
			Scope:     netlink.SCOPE_LINK,
		}); err != nil {
			return fmt.Errorf("set on-link route to gateway %s: %w", gateway, err)
		}

		if err := routeReplace(&netlink.Route{
			LinkIndex: containerLink.Attrs().Index,
			Gw:        addrToNetIP(gateway),
		}); err != nil {
			return fmt.Errorf("set default route through %s: %w", gateway, err)
		}

		return nil
	})
	if err != nil {
		return Veth{}, cleanupVeth(hostName, err)
	}

	hostLink, err := linkByName(hostName)
	if err != nil {
		return Veth{}, cleanupVeth(hostName, fmt.Errorf("lookup host veth %q: %w", hostName, err))
	}

	hostIfIndex := hostLink.Attrs().Index
	if hostIfIndex <= 0 {
		return Veth{}, cleanupVeth(hostName, fmt.Errorf("host veth %q has invalid ifindex %d", hostName, hostIfIndex))
	}

	if err := linkSetMTU(hostLink, mtu); err != nil {
		return Veth{}, cleanupVeth(hostName, fmt.Errorf("set host veth MTU: %w", err))
	}

	if err := linkSetUp(hostLink); err != nil {
		return Veth{}, cleanupVeth(hostName, fmt.Errorf("bring host veth up: %w", err))
	}

	if err := routeReplace(&netlink.Route{
		LinkIndex: hostIfIndex,
		Dst:       podNet,
		Scope:     netlink.SCOPE_LINK,
	}); err != nil {
		return Veth{}, cleanupVeth(hostName, fmt.Errorf("set host route to pod %s: %w", podIP, err))
	}

	if len(hostLink.Attrs().HardwareAddr) == 0 {
		return Veth{}, cleanupVeth(
			hostName,
			fmt.Errorf("host veth %q has no MAC address for synthetic gateway neighbor", hostName),
		)
	}
	hostMAC := append(net.HardwareAddr(nil), hostLink.Attrs().HardwareAddr...)

	if err := netnsHandle.Do(func(_ ns.NetNS) error {
		containerLink, err := linkByName(ifName)
		if err != nil {
			return fmt.Errorf("lookup container veth %q for neighbor: %w", ifName, err)
		}

		if err := neighborSet(&netlink.Neigh{
			LinkIndex:    containerLink.Attrs().Index,
			IP:           addrToNetIP(gateway),
			HardwareAddr: hostMAC,
			State:        netlink.NUD_PERMANENT,
		}); err != nil {
			return fmt.Errorf("set static gateway neighbor %s: %w", gateway, err)
		}

		return nil
	}); err != nil {
		return Veth{}, cleanupVeth(hostName, err)
	}

	return Veth{
		HostName:    hostName,
		HostIfIndex: hostIfIndex,
	}, nil
}

// DeleteVeth deletes the pod-side interface.
//
// An empty or missing network namespace is treated as already deleted. When a
// pod network namespace disappears, the kernel also removes its veth peer.
func (n *Linux) DeleteVeth(netnsPath, ifName string) error {
	if err := validateInterfaceName(ifName); err != nil {
		return err
	}
	if netnsPath == "" {
		return nil
	}

	if _, err := os.Stat(netnsPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat netns %q: %w", netnsPath, err)
	}

	netnsHandle, err := getNS(netnsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open netns %q: %w", netnsPath, err)
	}
	defer netnsHandle.Close()

	return netnsHandle.Do(func(_ ns.NetNS) error {
		link, err := linkByName(ifName)
		if err != nil {
			if errors.Is(err, syscall.ENOENT) {
				return nil
			}
			return fmt.Errorf("lookup veth %q: %w", ifName, err)
		}

		if err := linkDelete(link); err != nil {
			if errors.Is(err, syscall.ENOENT) {
				return nil
			}
			return fmt.Errorf("delete veth %q: %w", ifName, err)
		}

		return nil
	})
}

// CheckVeth verifies that ifName exists in netnsPath and has podIP assigned.
func (n *Linux) CheckVeth(netnsPath, ifName string, podIP netip.Addr) error {
	if netnsPath == "" {
		return errors.New("network: empty netns path")
	}
	if err := validateInterfaceName(ifName); err != nil {
		return err
	}

	podIP = podIP.Unmap()
	if !podIP.IsValid() || !podIP.Is4() || podIP.Zone() != "" {
		return fmt.Errorf("network: invalid IPv4 pod address %s", podIP)
	}

	netnsHandle, err := getNS(netnsPath)
	if err != nil {
		return fmt.Errorf("open netns %q: %w", netnsPath, err)
	}
	defer netnsHandle.Close()

	return netnsHandle.Do(func(_ ns.NetNS) error {
		link, err := linkByName(ifName)
		if err != nil {
			return fmt.Errorf("lookup veth %q: %w", ifName, err)
		}

		addresses, err := addrList(link, netlink.FAMILY_V4)
		if err != nil {
			return fmt.Errorf("list addresses for %q: %w", ifName, err)
		}

		for _, address := range addresses {
			if address.IP == nil {
				continue
			}

			got, ok := netip.AddrFromSlice(address.IP)
			if ok && got.Unmap() == podIP {
				return nil
			}
		}

		return fmt.Errorf("pod address %s is not present on %q", podIP, ifName)
	})
}

func validateSetupVeth(
	netnsPath string,
	ifName string,
	podIP netip.Addr,
	gateway netip.Addr,
	mtu int,
) error {
	if netnsPath == "" {
		return errors.New("network: empty netns path")
	}
	if err := validateInterfaceName(ifName); err != nil {
		return err
	}

	podIP = podIP.Unmap()
	if !podIP.IsValid() || !podIP.Is4() || podIP.Zone() != "" {
		return fmt.Errorf("network: invalid IPv4 pod address %s", podIP)
	}

	gateway = gateway.Unmap()
	if !gateway.IsValid() || !gateway.Is4() || gateway.Zone() != "" {
		return fmt.Errorf("network: invalid IPv4 gateway %s", gateway)
	}

	if mtu <= 0 {
		return fmt.Errorf("network: invalid MTU %d", mtu)
	}

	return nil
}

func validateInterfaceName(ifName string) error {
	if ifName == "" {
		return errors.New("network: empty interface name")
	}
	if len(ifName) > maxInterfaceNameLength {
		return fmt.Errorf(
			"network: interface name %q is longer than %d characters",
			ifName,
			maxInterfaceNameLength,
		)
	}
	return nil
}

func cleanupVeth(hostName string, setupErr error) error {
	if hostName == "" {
		return setupErr
	}

	link, err := linkByName(hostName)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return setupErr
		}
		return errors.Join(setupErr, fmt.Errorf("cleanup host veth %q: lookup: %w", hostName, err))
	}

	if err := linkDelete(link); err != nil && !errors.Is(err, syscall.ENOENT) {
		return errors.Join(setupErr, fmt.Errorf("cleanup host veth %q: delete: %w", hostName, err))
	}

	return setupErr
}

func ipv4HostNetwork(addr netip.Addr) *net.IPNet {
	return &net.IPNet{
		IP:   addrToNetIP(addr),
		Mask: net.CIDRMask(32, 32),
	}
}

func addrToNetIP(addr netip.Addr) net.IP {
	addr = addr.Unmap()
	if addr.Is4() {
		v4 := addr.As4()
		return net.IPv4(v4[0], v4[1], v4[2], v4[3])
	}

	v6 := addr.As16()
	return net.IP(v6[:])
}

func prefixToIPNet(prefix netip.Prefix) *net.IPNet {
	prefix = prefix.Masked()
	address := prefix.Addr().Unmap()

	bits := 128
	if address.Is4() {
		bits = 32
	}

	return &net.IPNet{
		IP:   addrToNetIP(address),
		Mask: net.CIDRMask(prefix.Bits(), bits),
	}
}
