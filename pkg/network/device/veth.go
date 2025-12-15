package device

import (
	"errors"
	"fmt"
	"log"
	"net"
	"syscall"

	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

var (
	ipSetupVeth = ip.SetupVeth

	nlLinkByName    = netlink.LinkByName
	nlAddrAdd       = netlink.AddrAdd
	nlAddrList      = netlink.AddrList
	nlLinkSetMTU    = netlink.LinkSetMTU
	nlLinkSetUp     = netlink.LinkSetUp
	nlRouteAdd      = netlink.RouteAdd
	nlLinkSetMaster = netlink.LinkSetMaster
	nlLinkDel       = netlink.LinkDel
)

const maxIfNameLen = 15

// SetupVeth creates a veth pair for a pod:
//   - container end named ifName inside pod netns
//   - host end moved to host namespace and attached to bridgeName
//   - assigns podIPNet to container end, sets MTU, and default route via gateway.
//
// Arguments:
//   netnsPath  - path to target pod network namespace (e.g., /proc/<pid>/ns/net)
//   bridgeName - name of an existing Linux bridge in the host namespace
//   mtu        - MTU to set on both ends
//   ifName     - interface name inside the pod (<=15 chars)
//   podIPNet   - *net.IPNet for the pod (constructed by caller)
//   gateway    - default route gateway (bridge IP) inside the pod

func SetupVeth(
	netnsPath string,
	bridgeName string,
	mtu int,
	ifName string,
	podIPNet *net.IPNet,
	gateway net.IP,
) error {

	// ------------- Validation (outside container ns) -------------
	if netnsPath == "" {
		return errors.New("empty netnsPath")
	}
	if bridgeName == "" {
		return errors.New("empty bridgeName")
	}
	if ifName == "" {
		return errors.New("empty ifName")
	}
	if len(ifName) > maxIfNameLen {
		return fmt.Errorf("ifName %q longer than %d chars", ifName, maxIfNameLen)
	}
	if podIPNet == nil || podIPNet.IP == nil || podIPNet.Mask == nil {
		return fmt.Errorf("invalid podIPNet: %v", podIPNet)
	}
	if podIPNet.IP.To4() == nil {
		return fmt.Errorf("only IPv4 supported (pod IP %s)", podIPNet.IP)
	}
	if gateway == nil || gateway.To4() == nil {
		return fmt.Errorf("invalid gateway %v", gateway)
	}
	if !podIPNet.Contains(gateway) {
		return fmt.Errorf("gateway %s not in pod subnet %s", gateway, podIPNet.String())
	}
	if mtu <= 0 {
		return fmt.Errorf("invalid MTU %d", mtu)
	}
	log.Printf("SetupVeth: netns=%s bridge=%s ifName=%s podIP=%s gateway=%s", netnsPath, bridgeName, ifName, podIPNet.String(), gateway.String())

	// Lookup bridge once (host namespace).
	brLink, err := nlLinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("lookup bridge %q: %w", bridgeName, err)
	}

	// Resolve the network namespace handle from path.
	nsHandle, err := ns.GetNS(netnsPath)
	if err != nil {
		return fmt.Errorf("open netns %q: %w", netnsPath, err)
	}
	defer nsHandle.Close()

	var hostIfaceName string

	// ------------- Work inside the pod net namespace -------------
	err = nsHandle.Do(func(hostNS ns.NetNS) error {
		// Create veth pair; host end moves to hostNS.
		hostVeth, containerVeth, err := ipSetupVeth(ifName, mtu, "", hostNS)
		if err != nil {
			return fmt.Errorf("setup veth pair: %w", err)
		}
		hostIfaceName = hostVeth.Name

		// Retrieve container end link.
		conLink, err := nlLinkByName(containerVeth.Name)
		if err != nil {
			return fmt.Errorf("lookup container link %q: %w", containerVeth.Name, err)
		}

		// Add (idempotent) IP address.
		if needsAddr(conLink, podIPNet) {
			if err := nlAddrAdd(conLink, &netlink.Addr{IPNet: podIPNet}); err != nil && !isEExist(err) {
				return fmt.Errorf("add addr %s: %w", podIPNet, err)
			}
		}

		// Enforce MTU.
		if err := nlLinkSetMTU(conLink, mtu); err != nil {
			return fmt.Errorf("set MTU: %w", err)
		}

		// Bring container interface up.
		if err := nlLinkSetUp(conLink); err != nil {
			return fmt.Errorf("link up container veth: %w", err)
		}

		// Add default route (idempotent).
		rt := &netlink.Route{
			LinkIndex: conLink.Attrs().Index,
			Gw:        gateway,
		}
		if err := nlRouteAdd(rt); err != nil && !isEExist(err) {
			return fmt.Errorf("add default route via %s: %w", gateway, err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// ------------- Host namespace adjustments -------------
	hostVeth, err := nlLinkByName(hostIfaceName)
	if err != nil {
		log.Printf("SetupVeth: error host adjustments hostIface=%s err=%v", hostIfaceName, err)
		return fmt.Errorf("lookup host veth %q: %w", hostIfaceName, err)
	}

	// Enforce MTU (host side).
	if err := nlLinkSetMTU(hostVeth, mtu); err != nil {
		return fmt.Errorf("set host veth MTU: %w", err)
	}

	// Attach to bridge.
	if err := nlLinkSetMaster(hostVeth, brLink); err != nil {
		log.Printf("SetupVeth: error attach host veth=%s bridge=%s err=%v", hostVeth.Attrs().Name, bridgeName, err)
		return fmt.Errorf("attach %q to bridge %q: %w", hostVeth.Attrs().Name, bridgeName, err)
	}

	// Bring host side up.
	if err := nlLinkSetUp(hostVeth); err != nil {
		log.Printf("SetupVeth: error bring host veth up: %v", err)
		return fmt.Errorf("bring host veth up: %w", err)
	}

	return nil
}

// needsAddr checks whether podIPNet is already present.
func needsAddr(link netlink.Link, want *net.IPNet) bool {
	addrs, err := nlAddrList(link, netlink.FAMILY_V4)
	if err != nil {
		// If we can't list, assume we need to add.
		return true
	}
	for _, a := range addrs {
		if a.IPNet == nil {
			continue
		}
		if a.IPNet.IP.Equal(want.IP) && maskEqual(a.IPNet.Mask, want.Mask) {
			return false
		}
	}
	return true
}

// DelVeth deletes the interface named ifName inside the given network namespace.
// Idempotent: succeeds (returns nil) if the interface does not exist.

func DelVeth(netns ns.NetNS, ifName string) error {
	if netns == nil {
		return errors.New("DelVeth: nil netns")
	}
	if ifName == "" {
		return errors.New("DelVeth: empty ifName")
	}
	if len(ifName) > maxIfNameLen {
		return fmt.Errorf("DelVeth: ifName %q longer than %d chars", ifName, maxIfNameLen)
	}

	return netns.Do(func(_ ns.NetNS) error {
		link, err := nlLinkByName(ifName)
		if err != nil {
			// netlink returns syscall.ENOENT if link is missing
			if errors.Is(err, syscall.ENOENT) {
				return nil // already gone
			}
			return fmt.Errorf("DelVeth: lookup %q: %w", ifName, err)
		}
		if err := nlLinkDel(link); err != nil {
			return fmt.Errorf("DelVeth: delete %q: %w", ifName, err)
		}
		return nil
	})
}

// CheckVeth verifies that the interface ifName exists inside the netns
// and has the given pod IP assigned (IPv4). It does NOT verify the subnet mask.
// Returns error if not found or IP not present.
func CheckVeth(netns ns.NetNS, ifName string, podIP net.IP) error {
	if netns == nil {
		return errors.New("CheckVeth: nil netns")
	}
	if ifName == "" {
		return errors.New("CheckVeth: empty ifName")
	}
	if podIP == nil || podIP.To4() == nil {
		return fmt.Errorf("CheckVeth: invalid IPv4 podIP %v", podIP)
	}

	return netns.Do(func(_ ns.NetNS) error {
		link, err := nlLinkByName(ifName)
		if err != nil {
			return fmt.Errorf("CheckVeth: link %q: %w", ifName, err)
		}

		addrs, err := nlAddrList(link, netlink.FAMILY_V4)
		if err != nil {
			return fmt.Errorf("CheckVeth: list addrs for %q: %w", ifName, err)
		}
		for _, a := range addrs {
			if a.IP.Equal(podIP) {
				return nil
			}
		}
		return fmt.Errorf("CheckVeth: ip %s not present on %s", podIP, ifName)
	})
}

// CheckVethIPNet is a stricter variant that also validates the mask (if you care).
func CheckVethIPNet(netns ns.NetNS, ifName string, podIPNet *net.IPNet) error {
	if podIPNet == nil || podIPNet.IP == nil || podIPNet.Mask == nil {
		return errors.New("CheckVethIPNet: invalid podIPNet")
	}
	if podIPNet.IP.To4() == nil {
		return fmt.Errorf("CheckVethIPNet: only IPv4 supported (%s)", podIPNet.IP)
	}
	return netns.Do(func(_ ns.NetNS) error {
		link, err := nlLinkByName(ifName)
		if err != nil {
			return fmt.Errorf("CheckVethIPNet: link %q: %w", ifName, err)
		}
		addrs, err := nlAddrList(link, netlink.FAMILY_V4)
		if err != nil {
			return fmt.Errorf("CheckVethIPNet: list addrs: %w", err)
		}
		for _, a := range addrs {
			if a.IPNet == nil {
				continue
			}
			if a.IPNet.IP.Equal(podIPNet.IP) && maskEqual(a.IPNet.Mask, podIPNet.Mask) {
				return nil
			}
		}
		return fmt.Errorf("CheckVethIPNet: %s not found on %s", podIPNet, ifName)
	})
}

// HasVeth returns true if ifName exists in the namespace (no IP checks).
func HasVeth(netns ns.NetNS, ifName string) (bool, error) {
	if netns == nil {
		return false, errors.New("HasVeth: nil netns")
	}
	if ifName == "" {
		return false, errors.New("HasVeth: empty ifName")
	}
	var found bool
	err := netns.Do(func(_ ns.NetNS) error {
		_, err := nlLinkByName(ifName)
		if err != nil {
			if errors.Is(err, syscall.ENOENT) {
				found = false
				return nil
			}
			return err
		}
		found = true
		return nil
	})
	return found, err
}

// maskEqual compares two net.IPMask without allocating.
func maskEqual(a, b net.IPMask) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isEExist considers syscall.EEXIST as 'already exists'.
func isEExist(err error) bool {
	return errors.Is(err, syscall.EEXIST)
}
