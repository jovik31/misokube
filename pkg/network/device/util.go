package device

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	config "github/setera/pkg"
	"net"
)

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
