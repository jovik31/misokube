package backend

import (

	// std
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"syscall"

	"github.com/vishvananda/netlink"

	// internal packages
	config "github/setera/pkg"
)

func CreateBridge(name string, mtu int, network *net.IPNet) (netlink.Link, *net.IPNet, error) {

	// ensure the bridge name is within the character limit
	bridgeName, err := GenerateDeviceName(config.BrPrefix, name)
	if err != nil {
		return nil, nil, err
	}

	// check if the bridge already exists
	if l, _ := netlink.LinkByName(bridgeName); l != nil {
		return l, nil, nil
	}

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:   bridgeName,
			MTU:    mtu,
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

	// ensure the network is not nil
	if network == nil {
		return nil, nil, err
	}

	// get first ip of network
	bridgeIP, err := FirstIP(network)
	if err != nil {
		return nil, nil, err
	}

	// add the IP address to the bridge
	if err := netlink.AddrAdd(dev, &netlink.Addr{IPNet: bridgeIP}); err != nil {
		return nil, nil, err
	}

	if err := netlink.LinkSetUp(dev); err != nil {
		return nil, nil, err
	}

	return dev, bridgeIP, nil
}

// using netlink to generate a device name within a certain character limit
func GenerateDeviceName(prefix string, name string) (string, error) {

	if len(prefix) > config.MaxDeviceNameLength {
		return "", fmt.Errorf("prefix %s exceeds max length %d", prefix, config.MaxDeviceNameLength)
	}

	hashLen := config.MaxDeviceNameLength - len(prefix)

	hash := sha1.New()
	hash.Write([]byte(name))

	hashedName := hex.EncodeToString(hash.Sum(nil))

	return prefix + hashedName[:hashLen], nil

}

func FirstIP(ip *net.IPNet) (*net.IPNet, error) {

	// extract the network address - ip.IP does not guarantee to be the network address
	netIP := ip.IP.Mask(ip.Mask)
	ipLen := len(netIP)

	// copy it to a slice
	first := make(net.IP, ipLen)
	copy(first, netIP)

	// create a new IPNet with the first IP and the same mask
	// Add 1 to the last byte, with carry
	for i := ipLen - 1; i >= 0; i-- {
		first[i]++
		if first[i] != 0 {
			break
		}
		// else overflowed, carry to next more significant byte
	}

	if !ip.Contains(first) {
		return nil, fmt.Errorf("first IP %s is not in the network %s", first, ip)
	}

	return &net.IPNet{
		IP:   first,
		Mask: ip.Mask,
	}, nil

}

func HostIP(ip *net.IPNet) (*net.IPNet, error) {

	// extract the network address - guaranteed to be a valid IP - ip.IP might not be the network address
	hostIP := ip.IP.Mask(ip.Mask)

	return &net.IPNet{
		IP:   hostIP,
		Mask: ip.Mask,
	}, nil

}
