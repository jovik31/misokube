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

// FDBEntry describes one static VXLAN forwarding database entry.
type FDBEntry struct {
	IfIndex  int
	RemoteIP netip.Addr
	MAC      net.HardwareAddr
}

// SetFDB creates or replaces a static FDB entry.
func (n *Linux) SetFDB(entry FDBEntry) error {
	if err := validateFDB(entry); err != nil {
		return err
	}

	if err := neighborSet(buildFDB(entry)); err != nil {
		return fmt.Errorf("set FDB entry for %s: %w", entry.RemoteIP, err)
	}
	return nil
}

// DeleteFDB deletes a static FDB entry. A missing entry is treated as already
// deleted.
func (n *Linux) DeleteFDB(entry FDBEntry) error {
	if err := validateFDB(entry); err != nil {
		return err
	}

	if err := neighborDelete(buildFDB(entry)); err != nil && !errors.Is(err, syscall.ENOENT) {
		return fmt.Errorf("delete FDB entry for %s: %w", entry.RemoteIP, err)
	}
	return nil
}

// ListFDB returns remote VXLAN FDB entries on IfIndex.
//
// Kernel-owned local/self entries without a remote destination IP are ignored;
// this leaves only entries of the same shape Setera programs with SetFDB.
func (n *Linux) ListFDB(ifIndex int) ([]FDBEntry, error) {
	if ifIndex <= 0 {
		return nil, fmt.Errorf("network: invalid FDB ifindex %d", ifIndex)
	}

	entries, err := neighborList(ifIndex, syscall.AF_BRIDGE)
	if err != nil {
		return nil, fmt.Errorf("list FDB for ifindex %d: %w", ifIndex, err)
	}

	out := make([]FDBEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IP == nil || len(entry.HardwareAddr) == 0 {
			continue
		}
		remoteIP, ok := netip.AddrFromSlice(entry.IP)
		if !ok {
			continue
		}
		remoteIP = remoteIP.Unmap()
		if !remoteIP.Is4() || remoteIP.Zone() != "" {
			continue
		}

		out = append(out, FDBEntry{
			IfIndex:  entry.LinkIndex,
			RemoteIP: remoteIP,
			MAC:      append(net.HardwareAddr(nil), entry.HardwareAddr...),
		})
	}
	return out, nil
}

func validateFDB(entry FDBEntry) error {
	if entry.IfIndex <= 0 {
		return fmt.Errorf("network: invalid FDB ifindex %d", entry.IfIndex)
	}

	address := entry.RemoteIP.Unmap()
	if !address.IsValid() || address.Zone() != "" {
		return errors.New("network: invalid FDB remote address")
	}

	if len(entry.MAC) == 0 {
		return errors.New("network: FDB MAC is empty")
	}

	return nil
}

func buildFDB(entry FDBEntry) *netlink.Neigh {
	return &netlink.Neigh{
		LinkIndex:    entry.IfIndex,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           addrToNetIP(entry.RemoteIP),
		HardwareAddr: append(net.HardwareAddr(nil), entry.MAC...),
	}
}
