package backend

import (

	// std

	"fmt"
	"net"
	"syscall"

	"github.com/vishvananda/netlink"

	// internal packages
	config "github/setera/pkg"
)

func SetupBridge(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {

	// ensure the subnet is not nil
	if subnet == nil {
		return nil, nil, fmt.Errorf("subnet cannot be nil")
	}

	// ensure the bridge name is within the character limit
	bridgeName, err := GenerateDeviceName(config.BrPrefix, name)
	if err != nil {
		return nil, nil, err
	}

	// check if the bridge already exists
	if l, _ := netlink.LinkByName(bridgeName); l != nil {
		return l, nil, nil
	}

	// create the bridge
	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:   bridgeName,
			MTU:    config.DefaultMTU,
			TxQLen: -1,
		},
	}

	if err := netlink.LinkAdd(br); err != nil && err != syscall.EEXIST {
		return nil, nil, err

	}

	dev, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil, nil, err
	}

	// --------------------add an IP address to the bridge---------------------------------\\

	// get first ip of network
	bridgeIP, err := FirstIP(subnet)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get first IP of subnet %s: %w", subnet, err)
	}

	// add the IP address to the bridge
	if err := netlink.AddrReplace(dev, &netlink.Addr{IPNet: bridgeIP}); err != nil {
		return nil, nil, fmt.Errorf("failed to add IP address %s to bridge %s: %w", bridgeIP, bridgeName, err)
	}

	// bring the bridge up
	if err := netlink.LinkSetUp(dev); err != nil {
		return br, bridgeIP, fmt.Errorf("failed to set bridge %s up: %w", bridgeName, err)
	}

	return dev, bridgeIP, nil
}
