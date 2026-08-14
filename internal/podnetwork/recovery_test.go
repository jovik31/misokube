package podnetwork

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github/setera/internal/nodeipam"
	"github/setera/pkg/network"
)

func TestRecover(t *testing.T) {
	ipam := &fakeNodeIPAM{
		allocations: []nodeipam.Allocation{
			{
				IP:       netip.MustParseAddr("10.244.1.10"),
				PodUID:   "pod-a",
				TenantID: "tenant-a",
			},
		},
	}
	networkOps := &fakeNetwork{
		vethByIP: map[netip.Addr]network.Veth{
			netip.MustParseAddr("10.244.1.10"): {
				HostName:    "veth-a",
				HostIfIndex: 42,
			},
		},
	}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, networkOps, datapath)

	stats, err := configurator.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if stats.Recovered != 1 || stats.MissingVeth != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if datapath.recoverCalls != 1 {
		t.Fatalf("RecoverLocalPod called %d times, want 1", datapath.recoverCalls)
	}

	got := datapath.recoveredPod
	if got.IP != netip.MustParseAddr("10.244.1.10") ||
		got.PodUID != "pod-a" ||
		got.TenantID != "tenant-a" ||
		got.HostVethName != "veth-a" ||
		got.HostVethIfIndex != 42 {
		t.Fatalf("recovered Pod = %+v", got)
	}
}

func TestRecoverSkipsMissingVeth(t *testing.T) {
	ipam := &fakeNodeIPAM{
		allocations: []nodeipam.Allocation{
			{
				IP:       netip.MustParseAddr("10.244.1.10"),
				PodUID:   "pod-a",
				TenantID: "tenant-a",
			},
		},
	}
	networkOps := &fakeNetwork{findErr: network.ErrPodVethNotFound}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, networkOps, datapath)

	stats, err := configurator.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Recovered != 0 || stats.MissingVeth != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if datapath.recoverCalls != 0 {
		t.Fatalf("RecoverLocalPod called %d times, want 0", datapath.recoverCalls)
	}
}

func TestRecoverReturnsVethDiscoveryError(t *testing.T) {
	wantErr := errors.New("route lookup failed")
	ipam := &fakeNodeIPAM{
		allocations: []nodeipam.Allocation{
			{
				IP:       netip.MustParseAddr("10.244.1.10"),
				PodUID:   "pod-a",
				TenantID: "tenant-a",
			},
		},
	}

	configurator := newTestConfigurator(
		t,
		ipam,
		&fakeNetwork{findErr: wantErr},
		&fakeDatapath{},
	)

	_, err := configurator.Recover(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}

func TestRecoverReturnsDatapathError(t *testing.T) {
	wantErr := errors.New("recover failed")
	ipam := &fakeNodeIPAM{
		allocations: []nodeipam.Allocation{
			{
				IP:       netip.MustParseAddr("10.244.1.10"),
				PodUID:   "pod-a",
				TenantID: "tenant-a",
			},
		},
	}
	networkOps := &fakeNetwork{
		vethByIP: map[netip.Addr]network.Veth{
			netip.MustParseAddr("10.244.1.10"): {
				HostName:    "veth-a",
				HostIfIndex: 42,
			},
		},
	}
	datapath := &fakeDatapath{recoverErr: wantErr}

	configurator := newTestConfigurator(t, ipam, networkOps, datapath)

	_, err := configurator.Recover(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}

func TestRecoverHonorsCanceledContext(t *testing.T) {
	ipam := &fakeNodeIPAM{
		allocations: []nodeipam.Allocation{
			{
				IP:       netip.MustParseAddr("10.244.1.10"),
				PodUID:   "pod-a",
				TenantID: "tenant-a",
			},
		},
	}
	configurator := newTestConfigurator(
		t,
		ipam,
		&fakeNetwork{},
		&fakeDatapath{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := configurator.Recover(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
