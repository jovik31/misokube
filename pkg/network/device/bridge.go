package device

import (

	// std

	"fmt"
	"net"

	// internal packages
	config "github/setera/pkg"
	link_api "github/setera/pkg/network/devlink"
)

func SetupBridge(id string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {

	if subnet == nil {
		return nil, nil, fmt.Errorf("subnet cannot be nil")
	}

	bridgeName, err := GenerateDeviceName(config.BrPrefix, id)
	if err != nil {
		return nil, nil, fmt.Errorf("generate bridge name: %w", err)
	}

	link, ipnet, err := ensureBridge(bridgeName, subnet)
	if err != nil {
		return nil, nil, fmt.Errorf("ensure bridge: %w", err)
	}

	return link, ipnet, nil

}

func UpdateBridgeIP(device Device, subnet *net.IPNet) error {
	if device == nil {
		return fmt.Errorf("device cannot be nil")
	}

	// Check if the device is a base_device
	baseDev, ok := device.(*base_device)
	if !ok {
		return fmt.Errorf("device is not a base_device")
	}

	// Get the first IP of the subnet (network+1)
	ipNet, err := FirstIP(subnet)
	if err != nil {
		return fmt.Errorf("compute bridge IP: %w", err)
	}

	// get link by name
	lk := link.NewNetlinkAPI()
	link, err := lk.LinkByName(baseDev.GetName())
	if err != nil {
		return fmt.Errorf("get link by name %s: %w", baseDev.GetName(), err)
	}

	// Replace or add the address
	addr := &netlink.Addr{IPNet: ipNet}
	if err := lk.AddrReplace(link, addr); err != nil {
		return fmt.Errorf("replace bridge address: %w", err)
	}

	return nil
}

func DeleteBridge(device Device) error {
	if device == nil {
		return fmt.Errorf("device cannot be nil")
	}

	// Check if the device is a base_device
	baseDev, ok := device.(*base_device)
	if !ok {
		return fmt.Errorf("device is not a base_device")
	}

	// Get link by name
	link, err := netlink.LinkByName(baseDev.GetName())
	if err != nil {
		return fmt.Errorf("get link by name %s: %w", baseDev.GetName(), err)
	}

	// Delete the link
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete bridge link: %w", err)
	}

	return nil
}
