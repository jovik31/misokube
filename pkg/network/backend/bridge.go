package backend

import (

	// std
	"net"
	"net/netip"
	"syscall"

	"github.com/vishvananda/netlink"
)

func CreateBridge(bridgeName string, mtu int, gateway netip.Addr) (netlink.Link, error) {

	// check if the bridge already exists
	if l, _ := netlink.LinkByName(bridgeName); l != nil {
		return l, nil
	}

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:   bridgeName,
			MTU:    mtu,
			TxQLen: -1,
		},
	}

	if err := netlink.LinkAdd(br); err != nil && err != syscall.EEXIST {
		return nil, err
	}

	dev, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil, err
	}
	gatewayString := gateway.String()
	gatewayString = gatewayString + "/30"

	ip, ipnet, err := net.ParseCIDR(gatewayString)
	if err != nil {
		return nil, err
	}
	if err := netlink.AddrAdd(dev, &netlink.Addr{IPNet: &net.IPNet{IP: ip, Mask: ipnet.Mask}}); err != nil {
		return nil, err
	}

	if err := netlink.LinkSetUp(dev); err != nil {
		return nil, err
	}

	return dev, nil
}
