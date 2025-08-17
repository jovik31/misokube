//go:build unit

package device

import (
	"errors"
	"net"
	"reflect"
	"syscall"
	"testing"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

// ---- tiny fake ns ----
type fakeNS struct{}

func (fakeNS) Do(f func(ns.NetNS) error) error {
	if f != nil {
		return f(nil)
	}
	return nil
}
func (fakeNS) Set() error   { return nil }
func (fakeNS) Path() string { return "/proc/0/ns/net" }
func (fakeNS) Fd() uintptr  { return 0 }
func (fakeNS) Close() error { return nil }

// ---- shared hook save/restore (nl* only; ipSetupVeth handled via reflection stub) ----
type hooks struct {
	nlLinkByName    func(string) (netlink.Link, error)
	nlAddrAdd       func(netlink.Link, *netlink.Addr) error
	nlAddrList      func(netlink.Link, int) ([]netlink.Addr, error)
	nlLinkSetMTU    func(netlink.Link, int) error
	nlLinkSetUp     func(netlink.Link) error
	nlRouteAdd      func(*netlink.Route) error
	nlLinkSetMaster func(netlink.Link, netlink.Link) error
	nlLinkDel       func(netlink.Link) error
}

func saveHooks() hooks {
	return hooks{
		nlLinkByName:    nlLinkByName,
		nlAddrAdd:       nlAddrAdd,
		nlAddrList:      nlAddrList,
		nlLinkSetMTU:    nlLinkSetMTU,
		nlLinkSetUp:     nlLinkSetUp,
		nlRouteAdd:      nlRouteAdd,
		nlLinkSetMaster: nlLinkSetMaster,
		nlLinkDel:       nlLinkDel,
	}
}
func restoreHooks(h hooks) {
	nlLinkByName = h.nlLinkByName
	nlAddrAdd = h.nlAddrAdd
	nlAddrList = h.nlAddrList
	nlLinkSetMTU = h.nlLinkSetMTU
	nlLinkSetUp = h.nlLinkSetUp
	nlRouteAdd = h.nlRouteAdd
	nlLinkSetMaster = h.nlLinkSetMaster
	nlLinkDel = h.nlLinkDel
}

// ---- ipSetupVeth reflective stub (version-agnostic) ----

// stubIpSetupVeth sets ipSetupVeth to a function with the SAME signature as in your env,
// returning hostName/contName via their .Name field (works for net.Interface or current.Interface).
func stubIpSetupVeth(t *testing.T, hostName, contName string) func() {
	t.Helper()

	fnVar := reflect.ValueOf(&ipSetupVeth).Elem()
	orig := fnVar.Interface() // original function (any type)
	fnType := fnVar.Type()    // func(string,int,string,ns.NetNS) (T1,T2,error)

	makeOne := func(typ reflect.Type, name string) reflect.Value {
		// typ may be a struct or a pointer to struct with exported field "Name"
		if typ.Kind() == reflect.Ptr {
			v := reflect.New(typ.Elem())
			f := v.Elem().FieldByName("Name")
			if f.IsValid() && f.CanSet() {
				f.SetString(name)
			}
			return v
		}
		v := reflect.New(typ).Elem()
		f := v.FieldByName("Name")
		if f.IsValid() && f.CanSet() {
			f.SetString(name)
		}
		return v
	}

	stub := reflect.MakeFunc(fnType, func(in []reflect.Value) []reflect.Value {
		out := make([]reflect.Value, fnType.NumOut())
		out[0] = makeOne(fnType.Out(0), hostName)
		out[1] = makeOne(fnType.Out(1), contName)
		out[2] = reflect.Zero(fnType.Out(2)) // error = nil
		return out
	})
	fnVar.Set(stub)

	return func() { fnVar.Set(reflect.ValueOf(orig)) }
}

// ---- helpers already provided in device/test_helpers_unit_test.go ----
// mustCIDR(t, "x.x.x.x/yy") and mustIP(t, "x.x.x.x")

// ---- tests ----

func TestSetupVeth_Validation(t *testing.T) {
	fns := fakeNS{}
	ipn := mustCIDR(t, "10.2.3.4/24")

	err := SetupVeth(nil, "br0", 1500, "eth0", ipn, net.IPv4(10, 2, 3, 1))
	assert.Error(t, err)
	err = SetupVeth(fns, "", 1500, "eth0", ipn, net.IPv4(10, 2, 3, 1))
	assert.Error(t, err)
	err = SetupVeth(fns, "br0", 1500, "", ipn, net.IPv4(10, 2, 3, 1))
	assert.Error(t, err)
	err = SetupVeth(fns, "br0", 1500, "thisnameislongerthan15", ipn, net.IPv4(10, 2, 3, 1))
	assert.Error(t, err)
	err = SetupVeth(fns, "br0", 1500, "eth0", nil, net.IPv4(10, 2, 3, 1))
	assert.Error(t, err)
	err = SetupVeth(fns, "br0", 1500, "eth0", mustCIDR(t, "fd00::1/64"), net.IPv4(10, 2, 3, 1))
	assert.Error(t, err)
	err = SetupVeth(fns, "br0", 1500, "eth0", ipn, nil)
	assert.Error(t, err)
	err = SetupVeth(fns, "br0", 1500, "eth0", ipn, net.ParseIP("1.2.3.4")) // not in subnet
	assert.Error(t, err)
	err = SetupVeth(fns, "br0", 0, "eth0", ipn, net.IPv4(10, 2, 3, 1))
	assert.Error(t, err)
}

func TestSetupVeth_HappyPath(t *testing.T) {
	h := saveHooks()
	defer restoreHooks(h)
	restore := stubIpSetupVeth(t, "vethXYZ", "eth0")
	defer restore()

	// Fake links
	br := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: "br-A", Index: 10}}
	cont := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 11}}
	host := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "vethXYZ", Index: 12}}

	// Stub link lookup
	nlLinkByName = func(name string) (netlink.Link, error) {
		switch name {
		case "br-A":
			return br, nil
		case "eth0":
			return cont, nil
		case "vethXYZ":
			return host, nil
		default:
			return nil, errors.New("not found")
		}
	}

	// AddrList -> empty so needsAddr returns true
	nlAddrList = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		return nil, nil
	}

	addrAdded := false
	nlAddrAdd = func(l netlink.Link, a *netlink.Addr) error {
		assert.Equal(t, "eth0", l.Attrs().Name)
		assert.Equal(t, "10.10.0.2/24", a.IPNet.String())
		addrAdded = true
		return nil
	}

	mtuSetCont, mtuSetHost := false, false
	nlLinkSetMTU = func(l netlink.Link, mut int) error {
		if l.Attrs().Name == "eth0" {
			mtuSetCont = true
		}
		if l.Attrs().Name == "vethXYZ" {
			mtuSetHost = true
		}
		return nil
	}

	upCont, upHost := false, false
	nlLinkSetUp = func(l netlink.Link) error {
		if l.Attrs().Name == "eth0" {
			upCont = true
		}
		if l.Attrs().Name == "vethXYZ" {
			upHost = true
		}
		return nil
	}

	routeAdded := false
	nlRouteAdd = func(r *netlink.Route) error {
		assert.Equal(t, 11, r.LinkIndex)
		assert.Equal(t, net.IPv4(10, 10, 0, 1).String(), r.Gw.String())
		routeAdded = true
		return nil
	}

	masterSet := false
	nlLinkSetMaster = func(l, m netlink.Link) error {
		assert.Equal(t, "vethXYZ", l.Attrs().Name)
		assert.Equal(t, "br-A", m.Attrs().Name)
		masterSet = true
		return nil
	}

	ipn := mustCIDR(t, "10.10.0.2/24")
	err := SetupVeth(fakeNS{}, "br-A", 1450, "eth0", ipn, net.IPv4(10, 10, 0, 1))
	assert.NoError(t, err)
	assert.True(t, addrAdded && mtuSetCont && upCont && routeAdded && mtuSetHost && masterSet && upHost)
}

func TestSetupVeth_IdempotentAddrAndRoute(t *testing.T) {
	h := saveHooks()
	defer restoreHooks(h)
	restore := stubIpSetupVeth(t, "vethXYZ", "eth0")
	defer restore()

	br := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: "br-A", Index: 10}}
	cont := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 111}}
	host := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "vethXYZ", Index: 112}}

	nlLinkByName = func(name string) (netlink.Link, error) {
		switch name {
		case "br-A":
			return br, nil
		case "eth0":
			return cont, nil
		case "vethXYZ":
			return host, nil
		default:
			return nil, errors.New("not found")
		}
	}

	// AddrList -> already has the IP (so no AddrAdd)
	nlAddrList = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		_, n, _ := net.ParseCIDR("10.10.0.2/24")
		return []netlink.Addr{{IPNet: n}}, nil
	}
	nlAddrAdd = func(_ netlink.Link, _ *netlink.Addr) error {
		t.Fatalf("AddrAdd should not be called when address exists")
		return nil
	}

	// RouteAdd returns EEXIST (should be OK)
	nlRouteAdd = func(_ *netlink.Route) error { return syscall.EEXIST }

	// No-op others
	nlLinkSetMTU = func(_ netlink.Link, _ int) error { return nil }
	nlLinkSetUp = func(_ netlink.Link) error { return nil }
	nlLinkSetMaster = func(_ netlink.Link, _ netlink.Link) error { return nil }

	ipn := mustCIDR(t, "10.10.0.2/24")
	err := SetupVeth(fakeNS{}, "br-A", 1450, "eth0", ipn, net.IPv4(10, 10, 0, 1))
	assert.NoError(t, err)
}

func TestDelVeth(t *testing.T) {
	h := saveHooks()
	defer restoreHooks(h)

	// Present case
	delCalled := false
	nlLinkByName = func(name string) (netlink.Link, error) {
		return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}, nil
	}
	nlLinkDel = func(l netlink.Link) error { delCalled = true; return nil }

	err := DelVeth(fakeNS{}, "eth0")
	assert.NoError(t, err)
	assert.True(t, delCalled)

	// Missing case -> ENOENT -> nil error
	nlLinkByName = func(name string) (netlink.Link, error) { return nil, syscall.ENOENT }
	err = DelVeth(fakeNS{}, "eth0")
	assert.NoError(t, err)

	// Validation
	err = DelVeth(nil, "eth0")
	assert.Error(t, err)
	err = DelVeth(fakeNS{}, "")
	assert.Error(t, err)
	err = DelVeth(fakeNS{}, "thisnameislongerthan15")
	assert.Error(t, err)
}

func TestCheckVeth(t *testing.T) {
	h := saveHooks()
	defer restoreHooks(h)

	// link exists with address 10.1.2.3
	nlLinkByName = func(name string) (netlink.Link, error) {
		return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}, nil
	}
	nlAddrList = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		return []netlink.Addr{{IPNet: mustCIDR(t, "10.1.2.3/24")}}, nil
	}
	err := CheckVeth(fakeNS{}, "eth0", net.IPv4(10, 1, 2, 3))
	assert.NoError(t, err)

	// not present
	nlAddrList = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		return []netlink.Addr{{IPNet: mustCIDR(t, "10.1.2.4/24")}}, nil
	}
	err = CheckVeth(fakeNS{}, "eth0", net.IPv4(10, 1, 2, 3))
	assert.Error(t, err)
}

func TestCheckVethIPNet(t *testing.T) {
	h := saveHooks()
	defer restoreHooks(h)

	nlLinkByName = func(name string) (netlink.Link, error) {
		return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}, nil
	}
	// exact match
	nlAddrList = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		return []netlink.Addr{{IPNet: mustCIDR(t, "10.1.2.3/24")}}, nil
	}
	err := CheckVethIPNet(fakeNS{}, "eth0", mustCIDR(t, "10.1.2.3/24"))
	assert.NoError(t, err)

	// mask mismatch -> error
	nlAddrList = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		return []netlink.Addr{{IPNet: mustCIDR(t, "10.1.2.3/25")}}, nil
	}
	err = CheckVethIPNet(fakeNS{}, "eth0", mustCIDR(t, "10.1.2.3/24"))
	assert.Error(t, err)
}

func TestHasVeth(t *testing.T) {
	h := saveHooks()
	defer restoreHooks(h)

	// present
	nlLinkByName = func(name string) (netlink.Link, error) {
		return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}, nil
	}
	found, err := HasVeth(fakeNS{}, "eth0")
	assert.NoError(t, err)
	assert.True(t, found)

	// not present -> ENOENT
	nlLinkByName = func(name string) (netlink.Link, error) { return nil, syscall.ENOENT }
	found, err = HasVeth(fakeNS{}, "eth0")
	assert.NoError(t, err)
	assert.False(t, found)

	// unexpected error
	nlLinkByName = func(name string) (netlink.Link, error) { return nil, errors.New("boom") }
	found, err = HasVeth(fakeNS{}, "eth0")
	assert.Error(t, err)
	assert.False(t, found)
}
