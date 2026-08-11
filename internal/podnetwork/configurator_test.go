package podnetwork

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github/setera/internal/nodeipam"
	"github/setera/pkg/network"
)

func TestAddPod(t *testing.T) {
	allocation := testAllocation()
	ipam := &fakeNodeIPAM{allocation: allocation}
	network := &fakeNetwork{
		veth: network.Veth{
			HostName:    "veth1234",
			HostIfIndex: 42,
		},
	}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	result, err := configurator.AddPod(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}

	if result.IP != allocation.IP {
		t.Fatalf("got IP %s, want %s", result.IP, allocation.IP)
	}
	if result.HostVethName != "veth1234" {
		t.Fatalf("got host veth %q, want veth1234", result.HostVethName)
	}
	if result.HostVethIfIndex != 42 {
		t.Fatalf("got host veth ifindex %d, want 42", result.HostVethIfIndex)
	}

	if ipam.allocateCalls != 1 {
		t.Fatalf("Allocate called %d times, want 1", ipam.allocateCalls)
	}
	if network.setupCalls != 1 {
		t.Fatalf("Setup called %d times, want 1", network.setupCalls)
	}
	if datapath.addCalls != 1 {
		t.Fatalf("AddLocalPod called %d times, want 1", datapath.addCalls)
	}

	if datapath.addedPod.IP != allocation.IP {
		t.Fatalf("datapath got IP %s, want %s", datapath.addedPod.IP, allocation.IP)
	}
	if datapath.addedPod.HostVethIfIndex != 42 {
		t.Fatalf("datapath got ifindex %d, want 42", datapath.addedPod.HostVethIfIndex)
	}
}

func TestAddPodRollsBackIPAMWhenNetworkFails(t *testing.T) {
	ipam := &fakeNodeIPAM{allocation: testAllocation()}
	network := &fakeNetwork{setupErr: errors.New("setup failed")}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	_, err := configurator.AddPod(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected ADD error")
	}

	if ipam.releaseCalls != 1 {
		t.Fatalf("Release called %d times, want 1", ipam.releaseCalls)
	}
	if datapath.addCalls != 0 {
		t.Fatalf("AddLocalPod called %d times, want 0", datapath.addCalls)
	}
}

func TestAddPodRollsBackNetworkAndIPAMWhenDatapathFails(t *testing.T) {
	ipam := &fakeNodeIPAM{allocation: testAllocation()}
	network := &fakeNetwork{
		veth: network.Veth{
			HostName:    "veth1234",
			HostIfIndex: 42,
		},
	}
	datapath := &fakeDatapath{addErr: errors.New("datapath failed")}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	_, err := configurator.AddPod(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected ADD error")
	}

	if network.deleteCalls != 1 {
		t.Fatalf("Delete called %d times, want 1", network.deleteCalls)
	}
	if ipam.releaseCalls != 1 {
		t.Fatalf("Release called %d times, want 1", ipam.releaseCalls)
	}
}

func TestDelPod(t *testing.T) {
	allocation := testAllocation()
	ipam := &fakeNodeIPAM{
		allocation: allocation,
		hasOwner:   true,
	}
	network := &fakeNetwork{}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	if err := configurator.DelPod(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}

	if datapath.deleteCalls != 1 {
		t.Fatalf("DeleteLocalPod called %d times, want 1", datapath.deleteCalls)
	}
	if datapath.deletedIP != allocation.IP {
		t.Fatalf("datapath deleted IP %s, want %s", datapath.deletedIP, allocation.IP)
	}
	if network.deleteCalls != 1 {
		t.Fatalf("Delete called %d times, want 1", network.deleteCalls)
	}
	if ipam.releaseCalls != 1 {
		t.Fatalf("Release called %d times, want 1", ipam.releaseCalls)
	}
}

func TestDelPodUnknownOwnerIsIdempotent(t *testing.T) {
	ipam := &fakeNodeIPAM{}
	network := &fakeNetwork{}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	if err := configurator.DelPod(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}

	if datapath.deleteCalls != 0 || network.deleteCalls != 0 || ipam.releaseCalls != 0 {
		t.Fatal("DEL changed state for an unknown owner")
	}
}

func TestDelPodKeepsAllocationWhenCleanupFails(t *testing.T) {
	ipam := &fakeNodeIPAM{
		allocation: testAllocation(),
		hasOwner:   true,
	}
	network := &fakeNetwork{deleteErr: errors.New("delete failed")}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	if err := configurator.DelPod(context.Background(), testRequest()); err == nil {
		t.Fatal("expected DEL error")
	}

	if ipam.releaseCalls != 0 {
		t.Fatalf("Release called %d times, want 0", ipam.releaseCalls)
	}
}

func TestDelPodAllowsEmptyNetNS(t *testing.T) {
	allocation := testAllocation()
	ipam := &fakeNodeIPAM{
		allocation: allocation,
		hasOwner:   true,
	}
	network := &fakeNetwork{}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	req := testRequest()
	req.NetNS = ""

	if err := configurator.DelPod(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	if network.lastDeleteNetNS != "" {
		t.Fatalf("Delete got netns %q, want empty", network.lastDeleteNetNS)
	}
}

func TestCheckPod(t *testing.T) {
	allocation := testAllocation()
	ipam := &fakeNodeIPAM{
		allocation: allocation,
		hasOwner:   true,
	}
	network := &fakeNetwork{}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	if err := configurator.CheckPod(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}

	if network.checkCalls != 1 {
		t.Fatalf("Check called %d times, want 1", network.checkCalls)
	}
	if network.lastCheckIP != allocation.IP {
		t.Fatalf("Check got IP %s, want %s", network.lastCheckIP, allocation.IP)
	}
}

func TestCheckPodRequiresAllocation(t *testing.T) {
	ipam := &fakeNodeIPAM{}
	network := &fakeNetwork{}
	datapath := &fakeDatapath{}

	configurator := newTestConfigurator(t, ipam, network, datapath)

	err := configurator.CheckPod(context.Background(), testRequest())
	if !errors.Is(err, ErrAllocationMissing) {
		t.Fatalf("expected ErrAllocationMissing, got %v", err)
	}

	if network.checkCalls != 0 {
		t.Fatalf("Check called %d times, want 0", network.checkCalls)
	}
}

func newTestConfigurator(
	t *testing.T,
	ipam nodeIPAM,
	network networkOps,
	datapath localDatapath,
) *Configurator {
	t.Helper()

	configurator, err := New(
		ipam,
		network,
		datapath,
		netip.MustParseAddr("169.254.1.1"),
		1500,
	)
	if err != nil {
		t.Fatal(err)
	}

	return configurator
}

func testRequest() Request {
	return Request{
		ContainerID: "container-a",
		NetNS:       "/var/run/netns/test",
		IfName:      "eth0",
		PodUID:      "pod-a",
		TenantID:    "tenant-a",
	}
}

func testAllocation() nodeipam.Allocation {
	return nodeipam.Allocation{
		IP: netip.MustParseAddr("10.244.0.7"),
		Owner: nodeipam.Owner{
			ContainerID: "container-a",
			IfName:      "eth0",
		},
		PodUID:   "pod-a",
		TenantID: "tenant-a",
	}
}

type fakeNodeIPAM struct {
	allocation  nodeipam.Allocation
	allocations []nodeipam.Allocation
	hasOwner    bool

	allocateErr error
	releaseErr  error

	allocateCalls int
	releaseCalls  int
}

func (f *fakeNodeIPAM) Allocate(
	_ context.Context,
	_ nodeipam.Request,
) (nodeipam.Allocation, error) {
	f.allocateCalls++
	if f.allocateErr != nil {
		return nodeipam.Allocation{}, f.allocateErr
	}
	f.hasOwner = true
	return f.allocation, nil
}

func (f *fakeNodeIPAM) Release(_ context.Context, _ nodeipam.Owner) error {
	f.releaseCalls++
	if f.releaseErr != nil {
		return f.releaseErr
	}
	f.hasOwner = false
	return nil
}

func (f *fakeNodeIPAM) Get(_ nodeipam.Owner) (nodeipam.Allocation, bool) {
	return f.allocation, f.hasOwner
}

func (f *fakeNodeIPAM) List() []nodeipam.Allocation {
	return append([]nodeipam.Allocation(nil), f.allocations...)
}

type fakeNetwork struct {
	veth network.Veth

	setupErr  error
	deleteErr error
	checkErr  error
	findErr   error

	vethByIP map[netip.Addr]network.Veth

	setupCalls  int
	deleteCalls int
	checkCalls  int

	lastDeleteNetNS string
	lastCheckIP     netip.Addr
}

func (f *fakeNetwork) SetupVeth(
	_, _ string,
	_, _ netip.Addr,
	_ int,
) (network.Veth, error) {
	f.setupCalls++
	if f.setupErr != nil {
		return network.Veth{}, f.setupErr
	}
	return f.veth, nil
}

func (f *fakeNetwork) DeleteVeth(netnsPath, _ string) error {
	f.deleteCalls++
	f.lastDeleteNetNS = netnsPath
	return f.deleteErr
}

func (f *fakeNetwork) CheckVeth(_, _ string, podIP netip.Addr) error {
	f.checkCalls++
	f.lastCheckIP = podIP
	return f.checkErr
}

func (f *fakeNetwork) FindPodVeth(ip netip.Addr) (network.Veth, error) {
	if f.findErr != nil {
		return network.Veth{}, f.findErr
	}
	veth, ok := f.vethByIP[ip]
	if !ok {
		return network.Veth{}, network.ErrPodVethNotFound
	}
	return veth, nil
}

type fakeDatapath struct {
	addErr     error
	deleteErr  error
	recoverErr error

	addCalls     int
	deleteCalls  int
	recoverCalls int

	addedPod     LocalPod
	recoveredPod LocalPod
	deletedIP    netip.Addr
}

func (f *fakeDatapath) AddLocalPod(_ context.Context, pod LocalPod) error {
	f.addCalls++
	f.addedPod = pod
	return f.addErr
}

func (f *fakeDatapath) DeleteLocalPod(_ context.Context, ip netip.Addr) error {
	f.deleteCalls++
	f.deletedIP = ip
	return f.deleteErr
}

func (f *fakeDatapath) RecoverLocalPod(_ context.Context, pod LocalPod) error {
	f.recoverCalls++
	f.recoveredPod = pod
	return f.recoverErr
}
