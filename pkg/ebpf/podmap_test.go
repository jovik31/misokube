package ebpf

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
)

func TestUpsertPodEndpointLocal(t *testing.T) {
	ip := netip.MustParseAddr("10.244.1.10")

	var gotIP net.IP
	var gotTenant string
	var gotIfIndex int

	err := upsertPodEndpoint(
		ip,
		"tenant-a",
		42,
		func(ip net.IP, tenant string, ifIndex int) error {
			gotIP = append(net.IP(nil), ip...)
			gotTenant = tenant
			gotIfIndex = ifIndex
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !gotIP.Equal(net.ParseIP("10.244.1.10")) {
		t.Fatalf("IP = %v, want 10.244.1.10", gotIP)
	}
	if gotTenant != "tenant-a" {
		t.Fatalf("tenant = %q, want %q", gotTenant, "tenant-a")
	}
	if gotIfIndex != 42 {
		t.Fatalf("ifindex = %d, want 42", gotIfIndex)
	}
}

func TestUpsertPodEndpointRemote(t *testing.T) {
	ip := netip.MustParseAddr("10.244.2.20")

	var gotIfIndex int

	err := upsertPodEndpoint(
		ip,
		"tenant-a",
		RemotePodIfIndex,
		func(_ net.IP, _ string, ifIndex int) error {
			gotIfIndex = ifIndex
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if gotIfIndex != RemotePodIfIndex {
		t.Fatalf(
			"ifindex = %d, want %d",
			gotIfIndex,
			RemotePodIfIndex,
		)
	}
}

func TestUpsertPodEndpointAcceptsIPv4MappedAddress(t *testing.T) {
	ip := netip.MustParseAddr("::ffff:10.244.1.10")

	var gotIP net.IP

	err := upsertPodEndpoint(
		ip,
		"default",
		7,
		func(ip net.IP, _ string, _ int) error {
			gotIP = append(net.IP(nil), ip...)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !gotIP.Equal(net.ParseIP("10.244.1.10")) {
		t.Fatalf("IP = %v, want 10.244.1.10", gotIP)
	}
}

func TestUpsertPodEndpointRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		ip      netip.Addr
		tenant  string
		ifIndex int
	}{
		{
			name:    "invalid IP",
			ip:      netip.Addr{},
			tenant:  "tenant-a",
			ifIndex: 42,
		},
		{
			name:    "IPv6",
			ip:      netip.MustParseAddr("2001:db8::10"),
			tenant:  "tenant-a",
			ifIndex: 42,
		},
		{
			name:    "empty tenant",
			ip:      netip.MustParseAddr("10.244.1.10"),
			tenant:  "",
			ifIndex: 42,
		},
		{
			name:    "whitespace tenant",
			ip:      netip.MustParseAddr("10.244.1.10"),
			tenant:  "   ",
			ifIndex: 42,
		},
		{
			name:    "tenant exceeds BPF capacity",
			ip:      netip.MustParseAddr("10.244.1.10"),
			tenant:  strings.Repeat("a", maxPodMapTenantIDBytes+1),
			ifIndex: 42,
		},
		{
			name:    "zero ifindex",
			ip:      netip.MustParseAddr("10.244.1.10"),
			tenant:  "tenant-a",
			ifIndex: 0,
		},
		{
			name:    "unsupported negative ifindex",
			ip:      netip.MustParseAddr("10.244.1.10"),
			tenant:  "tenant-a",
			ifIndex: -2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false

			err := upsertPodEndpoint(
				tt.ip,
				tt.tenant,
				tt.ifIndex,
				func(_ net.IP, _ string, _ int) error {
					called = true
					return nil
				},
			)
			if err == nil {
				t.Fatal("expected error")
			}
			if called {
				t.Fatal("writer called for invalid input")
			}
		})
	}
}

func TestUpsertPodEndpointWrapsWriterError(t *testing.T) {
	wantErr := errors.New("map update failed")

	err := upsertPodEndpoint(
		netip.MustParseAddr("10.244.1.10"),
		"tenant-a",
		42,
		func(_ net.IP, _ string, _ int) error {
			return wantErr
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}

func TestDeletePodEndpoint(t *testing.T) {
	ip := netip.MustParseAddr("10.244.1.10")

	var gotIP net.IP

	err := deletePodEndpoint(
		ip,
		func(ip net.IP) error {
			gotIP = append(net.IP(nil), ip...)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !gotIP.Equal(net.ParseIP("10.244.1.10")) {
		t.Fatalf("IP = %v, want 10.244.1.10", gotIP)
	}
}

func TestDeletePodEndpointRejectsIPv6(t *testing.T) {
	called := false

	err := deletePodEndpoint(
		netip.MustParseAddr("2001:db8::10"),
		func(_ net.IP) error {
			called = true
			return nil
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("deleter called for invalid input")
	}
}

func TestDeletePodEndpointWrapsDeleterError(t *testing.T) {
	wantErr := errors.New("map delete failed")

	err := deletePodEndpoint(
		netip.MustParseAddr("10.244.1.10"),
		func(_ net.IP) error {
			return wantErr
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}
