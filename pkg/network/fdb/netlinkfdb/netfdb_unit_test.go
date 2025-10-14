package netlinkfdb

import (
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestAddFDB_SetCorrectFields(t *testing.T) {

	mock := NewMockNetlinkFDB()
	mock.linkIndex["vx1"] = 1
	mgrFDB := NewFDBManager(mock)

	entry := sampleEntry("vx1", net.IPv4(10, 0, 0, 1), "00:11:22:33:44:55")

	if err := mgrFDB.Add(entry); err != nil {
		t.Fatalf("failed to add entry %v", err)
	}

	// check if add has the new entry
	if len(mock.sets) != 1 {
		t.Fatalf("expected 1 NeighSet call, got %d", len(mock.sets))
	}

	ln := mock.sets[0]
	if ln.LinkIndex != 1 {
		t.Fatalf("link index = %d, expected %d", ln.LinkIndex, mock.linkIndex["vx1"])
	}
	if ln.Family != syscall.AF_BRIDGE {
		t.Fatalf("family is %d, wanted %d", ln.Family, syscall.AF_BRIDGE)
	}
	if ln.State != netlink.NUD_PERMANENT {
		t.Fatalf("got state %d, wanted %d", ln.State, netlink.NUD_PERMANENT)
	}
	if ln.Flags != netlink.NTF_SELF {
		t.Fatalf("flags are %d, wanted %d", ln.Flags, netlink.NTF_SELF)
	}

	// lacks check on the mac address
	if !ln.IP.Equal(entry.IP) {
		t.Fatalf("ip is %v, wanted %v", ln.IP, entry.IP)
	}

	if ln.HardwareAddr.String() != entry.Mac.String() {
		t.Fatalf("got mac address %s, wanted %s", ln.HardwareAddr.String(), entry.Mac.String())
	}
}

func TestAddFDB_DeviceNotFound(t *testing.T) {

	mock := NewMockNetlinkFDB()
	mgrFDB := NewFDBManager(mock)

	entry := sampleEntry("vx1", net.IPv4(10, 0, 0, 1), "00:11:22:33:44:55")

	if err := mgrFDB.Add(entry); err != nil {
		if !errors.Is(err, syscall.ENOENT) {
			t.Fatalf("expected ENOENT, got %v", err)
		}
	}

}

func TestUpdateFDB_Success(t *testing.T) {

	mock := NewMockNetlinkFDB()
	mock.linkIndex["vx1"] = 1
	mgrFDB := NewFDBManager(mock)

	//place an existing entry
	mock.lists = []netlink.Neigh{{
		LinkIndex:    1,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           net.IPv4(10, 0, 0, 1),
		HardwareAddr: net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
	}}

	updatedIP := net.IPv4(10, 0, 0, 2)
	entry := sampleEntry("vx1", updatedIP, "00:11:22:33:44:55")

	if err := mgrFDB.Update(entry); err != nil {
		t.Fatalf("failed to update entry %v", err)
	}

	if len(mock.sets) != 1 {
		t.Fatalf("expected 1 NeighSet call, got %d", len(mock.sets))
	}

	if !mock.sets[0].IP.Equal(updatedIP) {
		t.Fatalf("got %v, wanted %v", mock.sets[0].IP, updatedIP)
	}

}

func TestUpdateFDB_NoExistingEntry(t *testing.T) {

	mock := NewMockNetlinkFDB()
	mock.linkIndex["vx1"] = 1
	mgrFDB := NewFDBManager(mock)

	mock.lists = []netlink.Neigh{{
		LinkIndex:    1,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           net.IPv4(10, 0, 0, 1),
		HardwareAddr: net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
	}}

	entry := sampleEntry("vx1", net.IPv4(10, 0, 0, 1), "aa:bb:cc:dd:ee:ff")
	if err := mgrFDB.Update(entry); err == nil {
		t.Fatalf("expected an error for missing mac address, got nil")

	}

}

func TestUpdateFDB_DeviceNotFound(t *testing.T) {

	mock := NewMockNetlinkFDB()
	mock.linkIndex[""] = 1
	mgrFDB := NewFDBManager(mock)

	entry := sampleEntry("vx-missing", net.IPv4(10, 0, 0, 1), "aa:bb:cc:dd:ee:ff")
	err := mgrFDB.Update(entry)
	if !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("expected ENOENT, got %v", err)
	}

}

func TestDeleteFDB_Success(t *testing.T) {

	mock := NewMockNetlinkFDB()
	mock.linkIndex["vx1"] = 1
	mgrFDB := NewFDBManager(mock)

	entry := sampleEntry("vx1", net.IPv4(10, 0, 0, 1), "aa:bb:cc:dd:ee:ff")

	if err := mgrFDB.Delete(entry); err != nil {
		t.Fatalf("failed to delete entry %v", err)
	}

	if len(mock.dels) != 1 {
		t.Fatalf("expected 1 NeighDelete call, got %d", len(mock.dels))
	}

	if mock.dels[0].LinkIndex != 1 {
		t.Fatalf("got link index %d, wanted %d", mock.dels[0].LinkIndex, 1)
	}

	if !mock.dels[0].IP.Equal(entry.IP) {
		t.Fatalf("got ip %v, wanted %v", mock.dels[0].IP, entry.IP)
	}

	if mock.dels[0].HardwareAddr.String() != entry.Mac.String() {
		t.Fatalf("mac %s, expeceted %s", mock.dels[0].HardwareAddr.String(), entry.Mac.String())
	}
}
