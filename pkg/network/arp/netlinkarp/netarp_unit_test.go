package netlinkarp

import (
	"net"
	"syscall"
	"testing"

	"github/setera/pkg/network/arp"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestARP_Add_UsesSetUpsert(t *testing.T) {
	mock := NewMockARPNL()
	mock.linkIndex["veth0"] = 7
	mgr := NewARPManager(mock)

	entry := arp.ARPEntry{
		IP:     net.IPv4(10, 1, 2, 3),
		Device: "veth0",
		MAC:    net.HardwareAddr{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0xee},
	}

	if err := mgr.Add(entry); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if got := len(mock.sets); got != 1 {
		t.Fatalf("expected 1 NeighSet call, got %d (adds=%d)", got, len(mock.adds))
	}
	ne := mock.sets[0]
	if ne.LinkIndex != 7 || !ne.IP.Equal(entry.IP) || ne.HardwareAddr.String() != entry.MAC.String() {
		t.Fatalf("bad fields in NeighSet: %+v", ne)
	}
	if ne.Family != netlink.FAMILY_V4 {
		t.Fatalf("family = %d, want v4", ne.Family)
	}
	if ne.State != netlink.NUD_PERMANENT {
		t.Fatalf("state = %d, want %d", ne.State, netlink.NUD_PERMANENT)
	}
	if ne.Type != syscall.RTN_UNICAST {
		t.Fatalf("type = %d, want RTN_UNICAST", ne.Type)
	}
}

func TestARP_Update_SetCorrectFields(t *testing.T) {
	mock := NewMockARPNL()
	mock.linkIndex["veth1"] = 3
	mgr := NewARPManager(mock)

	entry := arp.ARPEntry{
		IP:     net.ParseIP("2001:db8::1"),
		Device: "veth1",
		MAC:    net.HardwareAddr{0x02, 0xff, 0xee, 0xdd, 0xcc, 0xbb},
	}

	if err := mgr.Update(entry); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if len(mock.sets) != 1 {
		t.Fatalf("expected 1 NeighSet call, got %d", len(mock.sets))
	}
	ne := mock.sets[0]
	if ne.LinkIndex != 3 || !ne.IP.Equal(entry.IP) || ne.HardwareAddr.String() != entry.MAC.String() {
		t.Fatalf("bad fields in NeighSet: %+v", ne)
	}
	if ne.Family != netlink.FAMILY_V6 {
		t.Fatalf("family = %d, want v6", ne.Family)
	}
	if ne.State != netlink.NUD_PERMANENT {
		t.Fatalf("state = %d, want %d", ne.State, netlink.NUD_PERMANENT)
	}
	if ne.Type != syscall.RTN_UNICAST {
		t.Fatalf("type = %d, want RTN_UNICAST", ne.Type)
	}
}

func TestARP_Delete_NoMAC_Idempotent(t *testing.T) {
	mock := NewMockARPNL()
	mock.linkIndex["veth2"] = 11
	mgr := NewARPManager(mock)

	entry := arp.ARPEntry{
		IP:     net.IPv4(10, 9, 8, 7),
		Device: "veth2",
		// MAC omitted intentionally
	}

	if err := mgr.Delete(entry); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if len(mock.dels) != 1 {
		t.Fatalf("expected 1 NeighDel call, got %d", len(mock.dels))
	}
	ne := mock.dels[0]
	if ne.LinkIndex != 11 || !ne.IP.Equal(entry.IP) {
		t.Fatalf("bad fields in NeighDel: %+v", ne)
	}
	if len(ne.HardwareAddr) != 0 {
		t.Fatalf("expected no MAC on delete when not provided, got %s", ne.HardwareAddr.String())
	}
}

func TestPickFamily(t *testing.T) {
	if got := pickFamily(nil); got != unix.AF_UNSPEC {
		t.Fatalf("nil ip family = %d, want AF_UNSPEC", got)
	}
	if got := pickFamily(net.IP{}); got != unix.AF_UNSPEC {
		t.Fatalf("empty ip family = %d, want AF_UNSPEC", got)
	}
	if got := pickFamily(net.IPv4(1, 2, 3, 4)); got != netlink.FAMILY_V4 {
		t.Fatalf("ipv4 family = %d, want FAMILY_V4", got)
	}
	if got := pickFamily(net.ParseIP("::ffff:10.0.0.1")); got != netlink.FAMILY_V4 {
		t.Fatalf("v4-mapped ipv6 family = %d, want FAMILY_V4", got)
	}
	if got := pickFamily(net.ParseIP("2001:db8::1")); got != netlink.FAMILY_V6 {
		t.Fatalf("ipv6 family = %d, want FAMILY_V6", got)
	}
}
