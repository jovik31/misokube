package routing

import (
	"log"
	"net"
	"os/exec"
	"syscall"

	"github.com/coreos/go-iptables/iptables"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

func GetDefaultGatewayInterface() (*net.Interface, error) {
	routes, err := netlink.RouteList(nil, syscall.AF_INET)
	if err != nil {
		return nil, errors.Wrap(err, "RouteList error")
	}

	for _, route := range routes {
		if route.Dst == nil || route.Dst.String() == "0.0.0.0/0" {
			if route.LinkIndex <= 0 {
				return nil, errors.Errorf("found default route but could not determine interface")
			}
			return net.InterfaceByIndex(route.LinkIndex)
		}
	}

	return nil, errors.Errorf("unable to find default route")
}

func GetIfaceAddr(iface *net.Interface) ([]netlink.Addr, error) {
	return netlink.AddrList(&netlink.Device{
		LinkAttrs: netlink.LinkAttrs{
			Index: iface.Index,
		},
	}, syscall.AF_INET)
}

func EnableIPForwarding() error {
	cmd := exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1")
	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "Failed to enable IP forwarding")
	}
	return nil
}

func AllowBridgeForward(bridgeInterface string) error {

	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		log.Printf("Error creating iptables: %s", err.Error())
		return err
	}
	//Add rule to allow forwarding from bridge to host
	if err := ipt.AppendUnique("filter", "FORWARD", "-i", bridgeInterface, "-j", "ACCEPT"); err != nil {
		log.Printf("Error adding iptables rule: %s", err.Error())
		return err
	}
	return nil
}
