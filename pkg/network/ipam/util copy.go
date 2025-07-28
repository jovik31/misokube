package ipam

import (
	"fmt"
	"net"
)

// addIPv4 returns base + delta (IPv4 only).
func addIPv4(base net.IP, delta int) net.IP {
	b := base.To4()
	if b == nil {
		return nil
	}
	out := make(net.IP, len(b))
	copy(out, b)
	v := (int(out[0])<<24 | int(out[1])<<16 | int(out[2])<<8 | int(out[3])) + delta
	out[0] = byte(v >> 24)
	out[1] = byte(v >> 16)
	out[2] = byte(v >> 8)
	out[3] = byte(v)
	return out
}

// ipv4Diff returns ip - base as an integer offset (IPv4 only).
func ipv4Diff(base, ip net.IP) int {
	return (int(ip[0])-int(base[0]))<<24 |
		(int(ip[1])-int(base[1]))<<16 |
		(int(ip[2])-int(base[2]))<<8 |
		(int(ip[3]) - int(base[3]))
}

func IPNet(ip net.IP, subnet *net.IPNet) (*net.IPNet, error) {

	ip = ip.To4() // Ensure it's IPv4
	if ip == nil {
		return nil, fmt.Errorf("IP cannot be nil and must be IPv4")
	}

	return &net.IPNet{IP: ip, Mask: subnet.Mask}, nil
}
