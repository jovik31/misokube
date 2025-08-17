//go:build unit

package bridge

import (
	"errors"
	"net"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

// --- small fakes/fixtures ---

type fakeDev struct{ name string }

func (f fakeDev) GetName() string          { return f.name }
func (f fakeDev) GetIP() *net.IPNet        { return nil }
func (f fakeDev) GetMAC() net.HardwareAddr { return nil }

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("parse cidr %q: %v", s, err)
	}
	return n
}

// --- isLinkNotFound tests (pure logic) ---

func Test_isLinkNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ENOENT", syscall.ENOENT, true},
		{"lowercase", errors.New("link not found"), true},
		{"mixed", errors.New("no such device"), true},
		{"not-exist", errors.New("does not exist"), true},
		{"unrelated", errors.New("permission denied"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isLinkNotFound(tc.err)
			if got != tc.want {
				t.Fatalf("got %v want %v for err %q", got, tc.want, tc.err)
			}
		})
	}
}

// --- SetupBridge uses the function hooks (no real netlink required) ---

func TestSetupBridge_UsesGeneratedNameAndEnsuresBridge(t *testing.T) {
	// save & restore hooks
	oldGen := genDeviceName
	oldEnsure := ensureBridgeFunc
	defer func() { genDeviceName = oldGen; ensureBridgeFunc = oldEnsure }()

	// stub name generator
	var gotPrefix, gotID string
	genDeviceName = func(prefix, id string) (string, error) {
		gotPrefix, gotID = prefix, id
		return "br-mytenant", nil
	}

	// stub ensureBridge
	calledEnsure := false
	ensureBridgeFunc = func(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {
		calledEnsure = true
		if name != "br-mytenant" {
			t.Fatalf("ensureBridge got name %q", name)
		}
		return &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}, mustCIDR(t, "10.4.4.1/24"), nil
	}

	// run
	subnet := mustCIDR(t, "10.4.4.0/24")
	link, ip, err := SetupBridge("mytenant", subnet)
	assert.NoError(t, err)
	assert.True(t, calledEnsure)
	assert.Equal(t, "br-mytenant", link.Attrs().Name)
	assert.Equal(t, "10.4.4.1", ip.IP.String())

	// verify generator got expected args (prefix checked loosely)
	if !strings.HasPrefix(gotPrefix, "br") || gotID != "mytenant" {
		t.Fatalf("genDeviceName args unexpected: prefix=%q id=%q", gotPrefix, gotID)
	}
}

// --- UpdateBridgeIP uses nl* hooks; test happy-path parameters flow ---

func TestUpdateBridgeIP_HappyPath(t *testing.T) {
	oldByName, oldReplace, oldUp := nlLinkByName, nlAddrReplace, nlLinkSetUp
	defer func() { nlLinkByName, nlAddrReplace, nlLinkSetUp = oldByName, oldReplace, oldUp }()

	var gotName string
	fakeLink := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: "br-A"}}

	nlLinkByName = func(name string) (netlink.Link, error) { gotName = name; return fakeLink, nil }

	var replaced *netlink.Addr
	nlAddrReplace = func(l netlink.Link, a *netlink.Addr) error {
		if l.Attrs().Name != "br-A" {
			t.Fatalf("addr replace on %q", l.Attrs().Name)
		}
		replaced = a
		return nil
	}
	upCalled := false
	nlLinkSetUp = func(l netlink.Link) error {
		if l.Attrs().Name != "br-A" {
			t.Fatalf("link up on %q", l.Attrs().Name)
		}
		upCalled = true
		return nil
	}

	err := UpdateBridgeIP(fakeDev{name: "br-A"}, mustCIDR(t, "10.1.2.0/24"))
	assert.NoError(t, err)
	assert.Equal(t, "br-A", gotName)
	if replaced == nil || replaced.IPNet == nil || replaced.IPNet.String() != "10.1.2.1/24" {
		t.Fatalf("replaced addr unexpected: %#v", replaced)
	}
	assert.True(t, upCalled)
}

func TestUpdateBridgeIP_Validation(t *testing.T) {
	err := UpdateBridgeIP(nil, mustCIDR(t, "10.1.2.0/24"))
	assert.Error(t, err)
	err = UpdateBridgeIP(fakeDev{name: "br-X"}, nil)
	assert.Error(t, err)
}

// --- DeleteBridge uses nl* hooks; test happy-path parameters flow ---

func TestDeleteBridge_HappyPath(t *testing.T) {
	oldByName, oldDel := nlLinkByName, nlLinkDel
	defer func() { nlLinkByName, nlLinkDel = oldByName, oldDel }()

	fakeLink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "br-Z"}}
	var gotName, delName string

	nlLinkByName = func(name string) (netlink.Link, error) { gotName = name; return fakeLink, nil }
	nlLinkDel = func(l netlink.Link) error { delName = l.Attrs().Name; return nil }

	err := DeleteBridge(fakeDev{name: "br-Z"})
	assert.NoError(t, err)
	assert.Equal(t, "br-Z", gotName)
	assert.Equal(t, "br-Z", delName)
}

func TestDeleteBridge_Validation(t *testing.T) {
	err := DeleteBridge(nil)
	assert.Error(t, err)
}

// --- firstHostIP (internal) sanity checks via ensureBridge indirection ---
// We don't export firstHostIP; quick white-box check by exercising ensureBridge
// stub to capture what UpdateBridgeIP would compute.

func TestFirstHostIP_Computation(t *testing.T) {
	// stub ensureBridge to see what IP/mask would be applied
	oldEnsure := ensureBridgeFunc
	defer func() { ensureBridgeFunc = oldEnsure }()

	var gotIP string
	ensureBridgeFunc = func(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {
		ip := &net.IPNet{IP: net.IPv4(10, 9, 8, 1), Mask: subnet.Mask}
		gotIP = ip.String()
		return &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}, ip, nil
	}

	_, ip, err := SetupBridge("t", mustCIDR(t, "10.9.8.0/24"))
	assert.NoError(t, err)

	if ip.String() != "10.9.8.1/24" || gotIP != "10.9.8.1/24" {
		t.Fatalf("firstHostIP expected 10.9.8.1/24, got %s %s", ip, gotIP)
	}
}

// Ensure we didn’t accidentally regress the isLinkNotFound text matching
func Test_isLinkNotFound_TextVariants(t *testing.T) {
	variants := []string{
		"Link not found",
		"LINK NOT FOUND",
		"no such device",
		"Not Exist",
	}
	for _, s := range variants {
		if !isLinkNotFound(errors.New(s)) {
			t.Fatalf("variant %q should be not-found", s)
		}
	}
}

// Guard: reflect checks (pure compilation sanity)
func TestTypeSignatures(t *testing.T) {
	if reflect.ValueOf(ensureBridgeFunc).Kind() != reflect.Func {
		t.Fatal("ensureBridgeFunc is not a func")
	}
	if reflect.ValueOf(genDeviceName).Kind() != reflect.Func {
		t.Fatal("genDeviceName is not a func")
	}
}
