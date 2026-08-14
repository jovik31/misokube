package ebpfmanager

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"

	cebpf "github.com/cilium/ebpf"
	"github.com/vishvananda/netlink"

	"github/setera/internal/podnetwork"
)

const (
	ebpfIntegrationEnv = "SETERA_EBPF_INTEGRATION"
	tcPodIDsPinnedPath = "/sys/fs/bpf/setera/tc/tc_podIDs"
)

// TestIntegrationLocalPodLifecycle validates the complete local eBPF manager
// path against the Linux kernel:
//
//	Manager.AddLocalPod
//	    -> pkg/ebpf.AttachPodProgram
//	    -> TC ingress/egress filters
//	    -> shared tc_podIDs entry
//
//	Manager.DeleteLocalPod
//	    -> tc_podIDs entry removed
//	    -> Setera filters detached
//	    -> clsact retained
//
// The test is opt-in because it requires root/eBPF/TC privileges.
//
// Build it on the development machine:
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
//	  go test -c ./internal/ebpfmanager -o ./bin/ebpfmanager.test
//
// Then copy and run the binary inside a privileged kind node:
//
//	docker cp ./bin/ebpfmanager.test \
//	  setera-cluster-worker:/root/ebpfmanager.test
//
//	docker exec -e SETERA_EBPF_INTEGRATION=1 \
//	  setera-cluster-worker \
//	  /root/ebpfmanager.test \
//	  -test.run TestIntegrationLocalPodLifecycle \
//	  -test.v
func TestIntegrationLocalPodLifecycle(t *testing.T) {
	if os.Getenv(ebpfIntegrationEnv) != "1" {
		t.Skip("set SETERA_EBPF_INTEGRATION=1 to run privileged eBPF integration tests")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root or equivalent eBPF/TC capabilities")
	}

	host, _ := addIntegrationVethPair(t)

	podIP := netip.MustParseAddr("198.18.0.254")
	pod := podnetwork.LocalPod{
		IP:              podIP,
		PodUID:          "integration-pod-uid",
		TenantID:        "tenant-a",
		HostVethName:    host.Attrs().Name,
		HostVethIfIndex: host.Attrs().Index,
	}

	manager := New()

	// Keep the shared map clean even if a previous interrupted test left the
	// chosen integration-test key behind.
	deletePinnedPodEndpointIfPresent(t, podIP)

	if err := manager.AddLocalPod(context.Background(), pod); err != nil {
		t.Fatalf("AddLocalPod: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.DeleteLocalPod(context.Background(), podIP)
	})

	requireLocalManagerRecord(t, manager, pod)
	requireSeteraPodFilters(t, host)
	requirePinnedPodEndpoint(
		t,
		podIP,
		pod.TenantID,
		pod.HostVethIfIndex,
	)

	if err := manager.DeleteLocalPod(context.Background(), podIP); err != nil {
		t.Fatalf("DeleteLocalPod: %v", err)
	}

	requireNoLocalManagerRecord(t, manager, podIP)
	requirePinnedPodEndpointMissing(t, podIP)
	requireNoSeteraPodFilters(t, host)
	requireClsact(t, host)

	t.Logf(
		"local Pod lifecycle verified: ip=%s tenant=%s hostVeth=%s ifindex=%d",
		pod.IP,
		pod.TenantID,
		pod.HostVethName,
		pod.HostVethIfIndex,
	)
}

func addIntegrationVethPair(
	t *testing.T,
) (netlink.Link, netlink.Link) {
	t.Helper()

	const (
		hostName = "st-mgr-host"
		peerName = "st-mgr-peer"
	)

	// Clean up interfaces left by an interrupted previous run.
	deleteLinkByName(hostName)
	deleteLinkByName(peerName)

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: hostName,
		},
		PeerName: peerName,
	}

	if err := netlink.LinkAdd(veth); err != nil {
		t.Fatalf("add integration veth pair: %v", err)
	}

	t.Cleanup(func() {
		deleteLinkByName(hostName)
		deleteLinkByName(peerName)
	})

	host, err := netlink.LinkByName(hostName)
	if err != nil {
		t.Fatalf("lookup host veth %s: %v", hostName, err)
	}
	peer, err := netlink.LinkByName(peerName)
	if err != nil {
		t.Fatalf("lookup peer veth %s: %v", peerName, err)
	}

	if err := netlink.LinkSetUp(host); err != nil {
		t.Fatalf("set host veth %s up: %v", hostName, err)
	}
	if err := netlink.LinkSetUp(peer); err != nil {
		t.Fatalf("set peer veth %s up: %v", peerName, err)
	}

	return host, peer
}

func deleteLinkByName(name string) {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return
	}
	_ = netlink.LinkDel(link)
}

func requireLocalManagerRecord(
	t *testing.T,
	manager *Manager,
	pod podnetwork.LocalPod,
) {
	t.Helper()

	manager.mu.Lock()
	defer manager.mu.Unlock()

	state, ok := manager.local[pod.IP.Unmap()]
	if !ok {
		t.Fatalf("manager does not track local Pod %s", pod.IP)
	}

	if state.pod.PodUID != pod.PodUID {
		t.Fatalf(
			"manager PodUID=%q, want %q",
			state.pod.PodUID,
			pod.PodUID,
		)
	}
	if state.pod.TenantID != pod.TenantID {
		t.Fatalf(
			"manager tenant=%q, want %q",
			state.pod.TenantID,
			pod.TenantID,
		)
	}
	if state.pod.HostVethName != pod.HostVethName {
		t.Fatalf(
			"manager host veth=%q, want %q",
			state.pod.HostVethName,
			pod.HostVethName,
		)
	}
	if state.pod.HostVethIfIndex != pod.HostVethIfIndex {
		t.Fatalf(
			"manager host ifindex=%d, want %d",
			state.pod.HostVethIfIndex,
			pod.HostVethIfIndex,
		)
	}
	if state.program == nil {
		t.Fatal("manager stored nil Pod program")
	}
}

func requireNoLocalManagerRecord(
	t *testing.T,
	manager *Manager,
	ip netip.Addr,
) {
	t.Helper()

	manager.mu.Lock()
	defer manager.mu.Unlock()

	if _, ok := manager.local[ip.Unmap()]; ok {
		t.Fatalf("manager still tracks local Pod %s", ip)
	}
}

func requireSeteraPodFilters(
	t *testing.T,
	link netlink.Link,
) {
	t.Helper()

	requireNamedBPFProgram(
		t,
		link,
		netlink.HANDLE_MIN_INGRESS,
		"setera_tc_ingress",
	)
	requireNamedBPFProgram(
		t,
		link,
		netlink.HANDLE_MIN_EGRESS,
		"setera_tc_egress",
	)
}

func requireNamedBPFProgram(
	t *testing.T,
	link netlink.Link,
	parent uint32,
	wantName string,
) {
	t.Helper()

	filters, err := netlink.FilterList(link, parent)
	if err != nil {
		t.Fatalf(
			"list filters on %s parent %#x: %v",
			link.Attrs().Name,
			parent,
			err,
		)
	}

	for _, filter := range filters {
		bpfFilter, ok := filter.(*netlink.BpfFilter)
		if !ok {
			continue
		}
		if bpfFilter.Name == wantName {
			return
		}
	}

	t.Fatalf(
		"BPF filter %q is not attached to %s parent %#x",
		wantName,
		link.Attrs().Name,
		parent,
	)
}

func requireNoSeteraPodFilters(
	t *testing.T,
	link netlink.Link,
) {
	t.Helper()

	for _, parent := range []uint32{
		netlink.HANDLE_MIN_INGRESS,
		netlink.HANDLE_MIN_EGRESS,
	} {
		filters, err := netlink.FilterList(link, parent)
		if err != nil {
			t.Fatalf(
				"list filters on %s parent %#x: %v",
				link.Attrs().Name,
				parent,
				err,
			)
		}

		for _, filter := range filters {
			bpfFilter, ok := filter.(*netlink.BpfFilter)
			if !ok {
				continue
			}
			if strings.HasPrefix(bpfFilter.Name, "setera_tc_") {
				t.Fatalf(
					"Setera Pod filter %q still attached to %s",
					bpfFilter.Name,
					link.Attrs().Name,
				)
			}
		}
	}
}

func requireClsact(
	t *testing.T,
	link netlink.Link,
) {
	t.Helper()

	qdiscs, err := netlink.QdiscList(link)
	if err != nil {
		t.Fatalf(
			"list qdiscs on %s: %v",
			link.Attrs().Name,
			err,
		)
	}

	for _, qdisc := range qdiscs {
		if qdisc.Type() == "clsact" {
			return
		}
	}

	t.Fatalf(
		"clsact qdisc was removed from %s",
		link.Attrs().Name,
	)
}

type integrationPodMapValue struct {
	Tenant      [64]byte
	VethIfIndex uint32
}

func requirePinnedPodEndpoint(
	t *testing.T,
	ip netip.Addr,
	wantTenant string,
	wantIfIndex int,
) {
	t.Helper()

	m := openPinnedPodMap(t)
	defer m.Close()

	key := podMapKey(ip)

	var value integrationPodMapValue
	if err := m.Lookup(key, &value); err != nil {
		t.Fatalf("lookup tc_podIDs[%s]: %v", ip, err)
	}

	gotTenant := string(value.Tenant[:])
	if i := strings.IndexByte(gotTenant, 0); i >= 0 {
		gotTenant = gotTenant[:i]
	}

	if gotTenant != wantTenant {
		t.Fatalf(
			"tc_podIDs[%s].tenant=%q, want %q",
			ip,
			gotTenant,
			wantTenant,
		)
	}
	if value.VethIfIndex != uint32(wantIfIndex) {
		t.Fatalf(
			"tc_podIDs[%s].ifindex=%d, want %d",
			ip,
			value.VethIfIndex,
			wantIfIndex,
		)
	}
}

func requirePinnedPodEndpointMissing(
	t *testing.T,
	ip netip.Addr,
) {
	t.Helper()

	m := openPinnedPodMap(t)
	defer m.Close()

	var value integrationPodMapValue
	err := m.Lookup(podMapKey(ip), &value)
	if !errors.Is(err, cebpf.ErrKeyNotExist) {
		if err == nil {
			t.Fatalf("tc_podIDs[%s] still exists", ip)
		}
		t.Fatalf("lookup deleted tc_podIDs[%s]: %v", ip, err)
	}
}

func deletePinnedPodEndpointIfPresent(
	t *testing.T,
	ip netip.Addr,
) {
	t.Helper()

	m, err := cebpf.LoadPinnedMap(tcPodIDsPinnedPath, nil)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf(
			"open pinned tc_podIDs map %s: %v",
			tcPodIDsPinnedPath,
			err,
		)
	}
	defer m.Close()

	if err := m.Delete(podMapKey(ip)); err != nil &&
		!errors.Is(err, cebpf.ErrKeyNotExist) {
		t.Fatalf("delete stale tc_podIDs[%s]: %v", ip, err)
	}
}

func openPinnedPodMap(
	t *testing.T,
) *cebpf.Map {
	t.Helper()

	m, err := cebpf.LoadPinnedMap(tcPodIDsPinnedPath, nil)
	if err != nil {
		t.Fatalf(
			"open pinned tc_podIDs map %s: %v",
			tcPodIDsPinnedPath,
			err,
		)
	}
	return m
}

// podMapKey returns the four raw IPv4 bytes used by the BPF map key.
//
// loader.WritePodTenantVeth converts those bytes through NativeEndian before
// updating a uint32 key, which serializes back to the same raw four bytes.
// Using [4]byte here therefore avoids host-endian assumptions in the test.
func podMapKey(ip netip.Addr) [4]byte {
	return ip.Unmap().As4()
}
