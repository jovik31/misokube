//go:build linux

package network

import (
	"fmt"
	"os"
)

const ipv4ForwardingPath = "/proc/sys/net/ipv4/ip_forward"

var writeFile = os.WriteFile

// EnableIPv4Forwarding enables IPv4 forwarding on the node.
func (n *Linux) EnableIPv4Forwarding() error {
	if err := writeFile(ipv4ForwardingPath, []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("enable IPv4 forwarding: %w", err)
	}
	return nil
}
