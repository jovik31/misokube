package ipam

import (
	"errors"
	"net"
	"testing"
)

//type expander interface {
//Expand(newSubnet *net.IPNet) error}

type factory struct {
	name    string
	newIPAM func(*net.IPNet) (IPAM, error)
}

// register ipam implementations
var implementations = []factory{
	{name: "bitmap", newIPAM: NewBitmapIPAM}}

type network_case struct {
	label     string
	cidr      string
	capacity  int
	remaining int
}

// register capacity test cases
var network_cases = []network_case{
	{label: "small", cidr: "10.0.0.0/30", capacity: 1, remaining: 1},     // /30 has 4 addresses, but 3 are reserved (network + broadcast + bridge)
	{label: "medium", cidr: "10.0.0.0/28", capacity: 13, remaining: 13},  // /28 has 16 addresses, but 3 are reserved (network + broadcast + bridge)
	{label: "large", cidr: "10.0.0.0/24", capacity: 253, remaining: 253}, // /24 has 256 addresses, but 3 are reserved (network + broadcast + bridge)
}

func TestNewIPAM_Errors(t *testing.T) {

	bad_cases := []struct {
		label string
		cidr  *net.IPNet
		err   error
	}{
		{"nil subnet", nil, ErrNilSubnet{}},
		{"ipv6 subnet", mustCIDR(t, "2001:db8::/64"), ErrIPv4OnlySupported{MaskSize: 128}},
		{"/32", mustCIDR(t, "10.0.0.0/32"), ErrSubnetTooSmall{Subnet: "10.0.0.0/32"}},
		{"/31", mustCIDR(t, "10.0.0.0/31"), ErrSubnetTooSmall{Subnet: "10.0.0.0/31"}},
	}

	forEachImpl(t, func(t *testing.T, impl factory) {

		for _, bc := range bad_cases {
			t.Run(bc.label, func(t *testing.T) {
				t.Helper()
				_, err := impl.newIPAM(bc.cidr)
				if !errors.Is(err, bc.err) {
					t.Errorf("expected error %v, got %v", bc.err, err)
				}
			})
		}

	})
}

func Test_Capacity_Errors(t *testing.T) {

	error_cases := []struct {
		label string
		cidr  *net.IPNet
		err   error
	}{
		{"nil subnet", nil, ErrNilSubnet{}},
	}

	// iterate over all capacity cases
	forEachImpl(t, func(t *testing.T, impl factory) {

		for _, ec := range error_cases {

			t.Run(ec.label, func(t *testing.T) {
				t.Helper()

				// create a new IPAM instance with nil subnet - bypass constructors
				a := &BitmapIPAM{
					Network: nil,
					bitmap:  []uint64{0, 0}, // size doesn’t matter here
					IPs:     make(map[string]*ContainerNetInfo),
				}
				_, err := a.Capacity()
				if !errors.Is(err, ErrNilSubnet{}) {
					t.Errorf("expected error %v, got %v", ErrNilSubnet{}, err)
				}
			})

		}
	})
}

func Test_Capacity(t *testing.T) {

	// iterate over all capacity cases
	forEachImpl(t, func(t *testing.T, impl factory) {

		for _, tc := range network_cases {

			t.Run(tc.label, func(t *testing.T) {
				t.Helper()
				sub := mustCIDR(t, tc.cidr)
				a, err := impl.newIPAM(sub)
				if err != nil {
					t.Fatalf("failed to create IPAM %s: %v", impl.name, err)
				}
				test_capacity(t, a, tc.capacity)
			})

		}
	})
}

func test_capacity(t *testing.T, a IPAM, wanted_capacity int) {
	t.Helper()
	capacity, err := a.Capacity()
	if err != nil {
		t.Fatalf("Capacity() failed: %v", err)
	}
	if capacity != wanted_capacity { // /30 has 4 addresses, but 3 are reserved (network + broadcast + bridge)
		t.Errorf("expected capacity %d, got %d", wanted_capacity, capacity)
	}
}

func Test_Remaining(t *testing.T) {

	// iterate over all capacity cases
	forEachImpl(t, func(t *testing.T, impl factory) {

		for _, tc := range network_cases {

			t.Run(tc.label, func(t *testing.T) {
				t.Helper()
				sub := mustCIDR(t, tc.cidr)
				a, err := impl.newIPAM(sub)
				if err != nil {
					t.Fatalf("failed to create IPAM %s: %v", impl.name, err)
				}
				test_remaining(t, a, tc.remaining)
			})

		}
	})

}

func test_remaining(t *testing.T, a IPAM, wanted_remaining int) {
	t.Helper()
	remaining, err := a.Remaining()
	if err != nil {
		t.Errorf("failed to retrive remaining IP addreses with error %s", err)
	}
	if remaining != wanted_remaining { // /30 has 4 addresses, but 2 are reserved (network + broadcast)
		t.Errorf("expected remaining %d, got %d", remaining, wanted_remaining)
	}
}

func Test_Allocate(t *testing.T) {

	subnet := mustCIDR(t, "10.0.0.0/30")
	forEachIPAM(t, subnet, func(t *testing.T, a IPAM) {

		test_allocate(t, a)

	})
}

func test_allocate(t *testing.T, a IPAM) {

	cap, err := a.Capacity()
	if err != nil {
		t.Fatalf("Capacity() failed: %v", err)
	}

	for i := 0; i < cap; i++ {
		_, err := a.Allocate("pod", "ctr", "eth0", "netns")
		if err != nil {
			t.Fatalf("AllocateIP #%d failed: %v", i, err)
		}
	}
	if rem, err := a.Remaining(); rem != 0 {
		if err != nil {
			t.Fatalf("failed to retrieve remaining IPs with error %s", err)
		}
		t.Errorf("Remaining after full alloc = %d; want 0", rem)
	}
}

/*func Test_Free(t *testing.T) {
	subnet := mustCIDR()

}*/

func forEachImpl(t *testing.T, fn func(t *testing.T, impl factory)) {

	// marks function as helper
	t.Helper()

	for _, impl := range implementations {
		t.Run(impl.name, func(t *testing.T) {
			fn(t, impl)
		})
	}
}

// iterates over all possible ipam implementatations
func forEachIPAM(t *testing.T, subnet *net.IPNet, fn func(t *testing.T, a IPAM)) {

	// marks function as helper
	t.Helper()

	for _, impl := range implementations {
		t.Run(impl.name, func(t *testing.T) {
			a, err := impl.newIPAM(subnet)
			if err != nil {
				t.Fatalf("failed to create IPAM %s: %v", impl.name, err)
			}
			fn(t, a)
		})
	}

}

// iterates over all capacity cases
func forEachNetwork(t *testing.T, cases []network_case, fn func(t *testing.T, sub *net.IPNet, val int)) {
	t.Helper()
	for _, tc := range cases {
		sub := mustCIDR(t, tc.cidr)
		t.Run(tc.label, func(t *testing.T) {
			t.Helper()
			fn(t, sub, tc.capacity)
		})
	}
}

// mustCIDR is a helper function that parses a CIDR string
func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, cidr, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("failed to parse CIDR %s: %v", s, err)
	}
	return cidr
}
