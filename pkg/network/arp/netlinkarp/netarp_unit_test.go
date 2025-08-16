package netlinkarp

import (
	"github/setera/pkg/network/arp"
	"net"
	"syscall"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestARP_Add_Update_Delete(t *testing.T) {
	mock := NewMockARPNL()
	mock.linkIndex["veth0"] = 7

	mgr := NewARPManager(mock)

	entry := arp.ARPEntry{
		IP:     net.ParseIP("10.1.2.3"),
		Device: "veth0",
		MAC:    net.HardwareAddr{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0xee},
		// Family optional; manager should infer from IP if zero
	}

	// ADD
	if err := mgr.Add(entry); err != nil {
		t.Fatal(err)
	}
	if len(mock.adds) != 1 {
		t.Fatalf("expected 1 NeighAdd, got %d", len(mock.adds))
	}
	add := mock.adds[0]
	if add.LinkIndex != 7 || !add.IP.Equal(entry.IP) || add.HardwareAddr.String() != entry.MAC.String() {
		t.Fatalf("bad add fields: %+v", add)
	}
	if add.State != netlink.NUD_PERMANENT || add.Type != syscall.RTN_UNICAST {
		t.Fatalf("state/type mismatch: %+v", add)
	}

	// UPDATE (replace)
	if err := mgr.Update(entry); err != nil {
		t.Fatal(err)
	}
	if len(mock.sets) != 1 {
		t.Fatalf("expected 1 NeighSet, got %d", len(mock.sets))
	}
	set := mock.sets[0]
	if set.LinkIndex != 7 || !set.IP.Equal(entry.IP) || set.HardwareAddr.String() != entry.MAC.String() {
		t.Fatalf("bad set fields: %+v", set)
	}

	// DELETE
	if err := mgr.Delete(entry); err != nil {
		t.Fatal(err)
	}
	if len(mock.dels) != 1 {
		t.Fatalf("expected 1 NeighDel, got %d", len(mock.dels))
	}
	del := mock.dels[0]
	if del.LinkIndex != 7 || !del.IP.Equal(entry.IP) || del.HardwareAddr.String() != entry.MAC.String() {
		t.Fatalf("bad del fields: %+v", del)
	}
}

func TestARP_Add_WithFamilyOverride(t *testing.T) {
	mock := NewMockARPNL()
	mock.linkIndex["veth1"] = 3

	mgr := NewARPManager(mock)

	entry := arp.ARPEntry{
		IP:     net.ParseIP("2001:db8::1"),
		Device: "veth1",
		MAC:    net.HardwareAddr{0x02, 0xff, 0xee, 0xdd, 0xcc, 0xbb},
		Family: netlink.FAMILY_V6, // explicit override
	}

	if err := mgr.Add(entry); err != nil {
		t.Fatal(err)
	}
	if len(mock.adds) != 1 {
		t.Fatalf("expected 1 NeighAdd, got %d", len(mock.adds))
	}
	if got := mock.adds[0].Family; got != netlink.FAMILY_V6 {
		t.Fatalf("family = %d, want v6", got)
	}
}
