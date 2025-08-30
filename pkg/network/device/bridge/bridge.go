package bridge

import (
	"fmt"
	"net"
	"strings"

	config "github/setera/pkg"         // BrPrefix
	"github/setera/pkg/network/device" // Device + FirstIP + GenerateDeviceName

	"github.com/vishvananda/netlink"
)

// ---------- test hooks (overridden in unit tests) ----------
var (
	genDeviceName = device.GenerateDeviceName

	nlLinkByName  = netlink.LinkByName
	nlLinkAdd     = netlink.LinkAdd
	nlAddrReplace = netlink.AddrReplace
	nlLinkSetUp   = netlink.LinkSetUp
	nlLinkDel     = netlink.LinkDel
)

// allow tests to swap ensureBridge if needed
var ensureBridgeFunc = ensureBridge

// SetupBridge ensures a Linux bridge for the tenant exists, has the first
// host IP of the tenant subnet (mask kept), and is UP. It returns the link
// and the IP/mask that was ensured.
func SetupBridge(id string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {
	if subnet == nil {
		return nil, nil, fmt.Errorf("subnet cannot be nil")
	}

	brName, err := genDeviceName(config.BrPrefix, id)
	if err != nil {
		return nil, nil, fmt.Errorf("generate bridge name: %w", err)
	}

	link, ipnet, err := ensureBridgeFunc(brName, subnet)
	if err != nil {
		return nil, nil, fmt.Errorf("ensure bridge: %w", err)
	}
	return link, ipnet, nil
}

// UpdateBridgeIP replaces/ensures the bridge address for the provided device
// to the first usable host IP of the subnet and brings the link to an up state.
func UpdateBridgeIP(d device.Device, subnet *net.IPNet) error {
	if d == nil {
		return fmt.Errorf("device cannot be nil")
	}
	if subnet == nil {
		return fmt.Errorf("subnet cannot be nil")
	}

	ipNet, err := device.FirstIP(subnet) // <- shared helper
	if err != nil {
		return fmt.Errorf("compute bridge IP: %w", err)
	}

	link, err := nlLinkByName(d.GetName())
	if err != nil {
		return fmt.Errorf("get link by name %q: %w", d.GetName(), err)
	}

	if err := nlAddrReplace(link, &netlink.Addr{IPNet: ipNet}); err != nil {
		return fmt.Errorf("replace bridge address: %w", err)
	}
	if err := nlLinkSetUp(link); err != nil {
		return fmt.Errorf("set link up: %w", err)
	}
	return nil
}

// DeleteBridge deletes the underlying kernel link for the provided device.
func DeleteBridge(d device.Device) error {
	if d == nil {
		return fmt.Errorf("device cannot be nil")
	}
	link, err := nlLinkByName(d.GetName())
	if err != nil {
		return fmt.Errorf("get link by name %q: %w", d.GetName(), err)
	}
	if err := nlLinkDel(link); err != nil {
		return fmt.Errorf("delete bridge link: %w", err)
	}
	return nil
}

// ---------- internals ----------

// ensureBridge gets or creates a bridge named 'name', ensures it has the first
// host IP of 'subnet' (keeping the subnet mask), and is set UP. Returns the
// link and the IP/mask applied.
func ensureBridge(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {
	l, err := nlLinkByName(name)
	switch {
	case err == nil:
		// must be a bridge
		if _, ok := l.(*netlink.Bridge); !ok {
			return nil, nil, fmt.Errorf("link %q exists but is not a bridge", name)
		}
	case isLinkNotFound(err):
		// create bridge
		br := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}
		if err := nlLinkAdd(br); err != nil {
			return nil, nil, fmt.Errorf("link add %q: %w", name, err)
		}
		var e error
		l, e = nlLinkByName(name)
		if e != nil {
			return nil, nil, fmt.Errorf("link lookup after add %q: %w", name, e)
		}
	default:
		return nil, nil, fmt.Errorf("lookup %q: %w", name, err)
	}

	ipn, err := device.FirstIP(subnet) // <- shared helper
	if err != nil {
		return nil, nil, err
	}

	if err := nlAddrReplace(l, &netlink.Addr{IPNet: ipn}); err != nil {
		return nil, nil, fmt.Errorf("addr replace: %w", err)
	}
	if err := nlLinkSetUp(l); err != nil {
		return nil, nil, fmt.Errorf("link up: %w", err)
	}
	return l, ipn, nil
}

// isLinkNotFound normalizes common "not found" errors from netlink.
func isLinkNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") ||
		strings.Contains(s, "no such device") ||
		strings.Contains(s, "does not exist")
}
