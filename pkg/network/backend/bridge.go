package backend

import (

	// std

	"bytes"
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/vishvananda/netlink"

	// internal packages
	config "github/setera/pkg"
)

func SetupBridge(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {

	if subnet == nil {
		return nil, nil, fmt.Errorf("subnet cannot be nil")
	}

	bridgeName, err := GenerateDeviceName(config.BrPrefix, name)
	if err != nil {
		return nil, nil, fmt.Errorf("generate device name: %w", err)
	}

	// Normalize subnet base
	networkBase := subnet.IP.Mask(subnet.Mask)

	// If already exists, ensure it has (or add) the expected IP (network+1)
	if existing, err := netlink.LinkByName(bridgeName); err == nil {
		bridgeIPNet, err := FirstIP(subnet) // network+1/mask
		if err != nil {
			return existing, nil, fmt.Errorf("compute bridge IP: %w", err)
		}

		// Check if address already present
		addrs, _ := netlink.AddrList(existing, netlink.FAMILY_V4)
		have := false
		for _, a := range addrs {
			if a.IPNet != nil &&
				a.IPNet.IP.Equal(bridgeIPNet.IP) &&
				bytes.Equal(a.IPNet.Mask, bridgeIPNet.Mask) {
				have = true
				break
			}
		}
		if !have {
			if err := netlink.AddrReplace(existing, &netlink.Addr{IPNet: bridgeIPNet}); err != nil {
				return existing, nil, fmt.Errorf("attach bridge addr: %w", err)
			}
		}
		_ = netlink.LinkSetUp(existing)
		return existing, bridgeIPNet, nil
	} else if !errors.Is(err, syscall.ENOENT) {
		return nil, nil, fmt.Errorf("lookup bridge %s: %w", bridgeName, err)
	}

	// Create the bridge
	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: bridgeName,
			MTU:  config.DefaultMTU,
			// TxQLen: 0 (omit; kernel default)
		},
	}
	if err := netlink.LinkAdd(br); err != nil && !errors.Is(err, syscall.EEXIST) {
		return nil, nil, fmt.Errorf("link add bridge: %w", err)
	}

	dev, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil, nil, fmt.Errorf("post-create lookup: %w", err)
	}

	bridgeIPNet, err := FirstIP(subnet) // network+1
	if err != nil {
		return dev, nil, fmt.Errorf("first host: %w", err)
	}

	if err := netlink.AddrReplace(dev, &netlink.Addr{IPNet: bridgeIPNet}); err != nil {
		return dev, nil, fmt.Errorf("assign addr %s: %w", bridgeIPNet, err)
	}

	if err := netlink.LinkSetUp(dev); err != nil {
		return dev, bridgeIPNet, fmt.Errorf("link set up: %w", err)
	}

	_ = networkBase // (just to show we normalized; remove if unused)
	return dev, bridgeIPNet, nil
}
