//go:build linux

package network

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"

	"github.com/vishvananda/netlink"
)

var neighborDelete = netlink.NeighDel

// Neighbor describes a permanent IP-to-MAC entry on one interface.
type Neighbor struct {
	IfIndex int
	IP      netip.Addr
	MAC     net.HardwareAddr
}

// SetNeighbor creates or replaces a permanent neighbor entry.
func (n *Linux) SetNeighbor(neighbor Neighbor) error {
	if err := validateNeighbor(neighbor, true); err != nil {
		return err
	}

	entry := buildNeighbor(neighbor)
	if err := neighborSet(entry); err != nil {
		return fmt.Errorf("set neighbor %s: %w", neighbor.IP, err)
	}
	return nil
}

// DeleteNeighbor deletes a neighbor entry. A missing entry is treated as
// already deleted.
func (n *Linux) DeleteNeighbor(neighbor Neighbor) error {
	if err := validateNeighbor(neighbor, false); err != nil {
		return err
	}

	entry := buildNeighbor(neighbor)
	if err := neighborDelete(entry); err != nil && !errors.Is(err, syscall.ENOENT) {
		return fmt.Errorf("delete neighbor %s: %w", neighbor.IP, err)
	}
	return nil
}

func validateNeighbor(neighbor Neighbor, requireMAC bool) error {
	if neighbor.IfIndex <= 0 {
		return fmt.Errorf("network: invalid neighbor ifindex %d", neighbor.IfIndex)
	}

	address := neighbor.IP.Unmap()
	if !address.IsValid() || address.Zone() != "" {
		return errors.New("network: invalid neighbor address")
	}

	if requireMAC && len(neighbor.MAC) == 0 {
		return errors.New("network: neighbor MAC is empty")
	}

	return nil
}

func buildNeighbor(neighbor Neighbor) *netlink.Neigh {
	address := neighbor.IP.Unmap()

	family := netlink.FAMILY_V6
	if address.Is4() {
		family = netlink.FAMILY_V4
	}

	return &netlink.Neigh{
		LinkIndex:    neighbor.IfIndex,
		Family:       family,
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           addrToNetIP(address),
		HardwareAddr: append(net.HardwareAddr(nil), neighbor.MAC...),
	}
}
