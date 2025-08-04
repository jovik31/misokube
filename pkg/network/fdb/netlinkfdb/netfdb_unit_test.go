package netlinkfdb

import (
	"github.com/vishvananda/netlink"
	"github/setera/pkg/network/fdb"
	"net"
	"syscall"
	"testing"
)

func TestAddFDB(t *testing.T) {

	mock := NewMockNetlinkFDB()
	mock.linkIndex["vx1"] = 1
	fdb.RegisterFDBManager(mock)

	entry := fdb.FDBEntry{
		Device: "vx1",
		Family: syscall.AF_BRIDGE,
		State:  netlink.NUD_PERMANENT,
		Flags:  netlink.NTF_SELF,
		IP:     net.IPv4(127, 0, 0, 1),
		Mac:    net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
	}

	if err := mgrFDB.Add(entry); err != nil {
		t.Fatal(err)
	}

	if len(mock.adds) != 1 {
		t.Fatal(len(mock.adds))
	}

	got := mock.adds[0]
	if got.LinkIndex != mock.linkIndex["vx1"] {
		t.Fatal(got.LinkIndex)
	}
	if got.LinkIndex != 1 {
		t.Fatal(got.LinkIndex)
	}
	// lacks check on the mac address

	if !got.IP.Equal(entry.IP) {
		t.Fatal(got.IP)
	}

}

/*func TestUpdateFDB(t *testing.T) {

	mock := &mockNetlinkFDB{linkIndex: map[string]int{"vx1": 1}}
	fdb := NewNetlinkFDB()
}*/
