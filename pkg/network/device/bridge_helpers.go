package device

import (
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	config "github/setera/pkg"
	"github/setera/pkg/network/devlink"
	"net"
	"strings"
	"syscall"

	//3rd party dependencies
	"github.com/vishvananda/netlink"
)

var (
	LinkByNameFunc = netlink.LinkByName
)

func ensureBridge(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error, link_api link.NetlinkAPI) {

	l, err := link_api.LinkByName(name)
	link, err := netlink.LinkByName(name)
	if err == nil {
		return ensureBridgeState(link, name, subnet)
	}

	if !isLinkNotFound(err) {
		return nil, nil, fmt.Errorf("lookup bridge link %s: %w", name, err)
	}

	return createBridge(name, subnet)

}

func ensureBridgeState(link netlink.Link, name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {

	ipnet, err := FirstIP(subnet)
	if err != nil {
		return link, nil, fmt.Errorf("compute bridge IP: %w", err)
	}

	if err := checkAddrIP(link, ipnet); err != nil {
		return link, nil, fmt.Errorf("check bridge IP: %w", err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return link, nil, fmt.Errorf("link set up: %w", err)
	}

	if link.Attrs().Name != name {
		if err := netlink.LinkSetName(link, name); err != nil {
			return link, nil, fmt.Errorf("set link name: %w", err)
		}
	}

	return nil, ipnet, nil
}

func createBridge(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {
	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: name,
			MTU:  config.DefaultMTU,
		},
	}

	if err := netlink.LinkAdd(br); err != nil && !errors.Is(err, syscall.EEXIST) {
		return nil, nil, fmt.Errorf("link add bridge: %w", err)
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return nil, nil, fmt.Errorf("lookup bridge: %w", err)
	}

	ipnet, err := FirstIP(subnet)
	if err != nil {
		return link, nil, fmt.Errorf("compute bridge IP: %w", err)
	}

	if err := netlink.AddrAdd(link, &netlink.Addr{IPNet: ipnet}); err != nil {
		return link, nil, fmt.Errorf("add bridge address: %w", err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return link, ipnet, fmt.Errorf("link set up: %w", err)
	}

	return link, ipnet, nil
}

func checkAddrIP(link netlink.Link, ipnet *net.IPNet) error {

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	for _, a := range addrs {
		if a.IPNet != nil && a.IPNet.IP.Equal(ipnet.IP) && bytes.Equal(a.IPNet.Mask, ipnet.Mask) {
			return nil
		}
	}

	return netlink.AddrReplace(link, &netlink.Addr{IPNet: ipnet})
}

func isLinkNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOENT) {
		return true
	}
	// Fallback on substring checks used by netlink library.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "link not found") ||
		strings.Contains(msg, "no such device") ||
		strings.Contains(msg, "not exist") || strings.Contains(msg, "Link not found")
}
