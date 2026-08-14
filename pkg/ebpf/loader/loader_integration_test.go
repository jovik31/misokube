package loader

import (
	"os"
	"strings"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/vishvananda/netlink"
)

// TestIntegrationTCProgramsShareTcPodIDs proves that a Pod TC program and the
// node router are loaded against the same kernel tc_podIDs map.
//
// This test is intentionally opt-in because it requires eBPF/TC privileges and
// creates temporary Linux interfaces.
//
// Run on a Setera development node with:
//
//	SETERA_EBPF_INTEGRATION=1 go test ./pkg/ebpf/loader -run TestIntegrationTCProgramsShareTcPodIDs -v
func TestIntegrationTCProgramsShareTcPodIDs(t *testing.T) {
	if os.Getenv("SETERA_EBPF_INTEGRATION") != "1" {
		t.Skip("set SETERA_EBPF_INTEGRATION=1 to run privileged eBPF integration tests")
	}

	if os.Geteuid() != 0 {
		t.Skip("requires root or equivalent eBPF/TC capabilities")
	}

	podLink := addIntegrationDummy(t, "setera-pod0")
	nodeLink := addIntegrationDummy(t, "setera-node0")

	podProgram, err := NewPodPolicy(
		podLink.Attrs().Name,
		"tenant-a",
	)
	if err != nil {
		t.Fatalf("attach Pod TC program: %v", err)
	}

	defer func() {
		if err := podProgram.Close(); err != nil {
			t.Errorf("close Pod TC program: %v", err)
		}
	}()

	nodeProgram, err := NewNodeRouter(
		nodeLink.Attrs().Name,
	)
	if err != nil {
		t.Fatalf("attach node router: %v", err)
	}

	defer func() {
		if err := nodeProgram.Close(); err != nil {
			t.Errorf("close node router: %v", err)
		}
	}()

	requireSeteraPodPolicy(t, podLink)
	requireSeteraIngressOnly(
		t,
		nodeLink,
		"setera_node_ingress",
	)

	podMapID := requireMapID(
		t,
		podProgram.firewall.objs.TcPodIDs,
	)

	nodeMapID := requireMapID(
		t,
		nodeProgram.objs.TcPodIDs,
	)

	podConfigMapID := requireMapID(
		t,
		podProgram.firewall.objs.TcIfaceCfg,
	)

	nodeConfigMapID := requireMapID(
		t,
		nodeProgram.objs.TcIfaceCfg,
	)

	podStatsMapID := requireMapID(
		t,
		podProgram.firewall.objs.TcStats,
	)

	nodeStatsMapID := requireMapID(
		t,
		nodeProgram.objs.TcStats,
	)

	pinned, err := ebpf.LoadPinnedMap(
		tcPodIDsMapPath,
		nil,
	)
	if err != nil {
		t.Fatalf(
			"open pinned tc_podIDs map %s: %v",
			tcPodIDsMapPath,
			err,
		)
	}
	defer pinned.Close()

	pinnedMapID := requireMapID(
		t,
		pinned,
	)

	if podMapID != pinnedMapID {
		t.Fatalf(
			"Pod TC tc_podIDs map ID = %d, pinned map ID = %d",
			podMapID,
			pinnedMapID,
		)
	}

	if nodeMapID != pinnedMapID {
		t.Fatalf(
			"node router tc_podIDs map ID = %d, pinned map ID = %d",
			nodeMapID,
			pinnedMapID,
		)
	}

	if podConfigMapID == nodeConfigMapID {
		t.Fatalf(
			"tc_iface_cfg unexpectedly shared: map ID=%d",
			podConfigMapID,
		)
	}

	if podStatsMapID == nodeStatsMapID {
		t.Fatalf(
			"tc_stats unexpectedly shared: map ID=%d",
			podStatsMapID,
		)
	}

	t.Logf(
		"tc_podIDs shared map ID=%d; private tc_iface_cfg IDs=%d/%d; private tc_stats IDs=%d/%d",
		pinnedMapID,
		podConfigMapID,
		nodeConfigMapID,
		podStatsMapID,
		nodeStatsMapID,
	)

	if err := podProgram.Close(); err != nil {
		t.Fatalf(
			"close Pod TC program: %v",
			err,
		)
	}

	if err := nodeProgram.Close(); err != nil {
		t.Fatalf(
			"close node router: %v",
			err,
		)
	}

	requireNoSeteraFilters(
		t,
		podLink,
	)

	requireNoSeteraFilters(
		t,
		nodeLink,
	)

	requireClsact(
		t,
		podLink,
	)

	requireClsact(
		t,
		nodeLink,
	)
}

func requireSeteraPodPolicy(
	t *testing.T,
	link netlink.Link,
) {
	t.Helper()

	requireNamedSeteraFilter(
		t,
		link,
		netlink.HANDLE_MIN_INGRESS,
		"setera_tc_ingress",
	)

	requireNamedSeteraFilter(
		t,
		link,
		netlink.HANDLE_MIN_EGRESS,
		"setera_tc_egress",
	)
}

func requireNamedSeteraFilter(
	t *testing.T,
	link netlink.Link,
	parent uint32,
	wantName string,
) {
	t.Helper()

	filters, err := netlink.FilterList(
		link,
		parent,
	)
	if err != nil {
		t.Fatalf(
			"list filters on %s parent %#x: %v",
			link.Attrs().Name,
			parent,
			err,
		)
	}

	found := false

	for _, filter := range filters {
		bpfFilter, ok :=
			filter.(*netlink.BpfFilter)

		if !ok ||
			!strings.HasPrefix(
				bpfFilter.Name,
				"setera_",
			) {
			continue
		}

		if bpfFilter.Name != wantName {
			t.Fatalf(
				"unexpected Setera filter %q on %s parent %#x",
				bpfFilter.Name,
				link.Attrs().Name,
				parent,
			)
		}

		found = true
	}

	if !found {
		t.Fatalf(
			"Setera filter %q not attached to %s parent %#x",
			wantName,
			link.Attrs().Name,
			parent,
		)
	}
}

func requireSeteraIngressOnly(
	t *testing.T,
	link netlink.Link,
	wantIngressName string,
) {
	t.Helper()

	ingressFilters, err := netlink.FilterList(
		link,
		netlink.HANDLE_MIN_INGRESS,
	)
	if err != nil {
		t.Fatalf(
			"list ingress filters on %s: %v",
			link.Attrs().Name,
			err,
		)
	}

	foundIngress := false

	for _, filter := range ingressFilters {
		bpfFilter, ok :=
			filter.(*netlink.BpfFilter)

		if !ok ||
			!strings.HasPrefix(
				bpfFilter.Name,
				"setera_",
			) {
			continue
		}

		if bpfFilter.Name != wantIngressName {
			t.Fatalf(
				"unexpected Setera ingress filter %q on %s",
				bpfFilter.Name,
				link.Attrs().Name,
			)
		}

		foundIngress = true
	}

	if !foundIngress {
		t.Fatalf(
			"Setera ingress filter %q not attached to %s",
			wantIngressName,
			link.Attrs().Name,
		)
	}

	egressFilters, err := netlink.FilterList(
		link,
		netlink.HANDLE_MIN_EGRESS,
	)
	if err != nil {
		t.Fatalf(
			"list egress filters on %s: %v",
			link.Attrs().Name,
			err,
		)
	}

	for _, filter := range egressFilters {
		bpfFilter, ok :=
			filter.(*netlink.BpfFilter)

		if !ok {
			continue
		}

		if strings.HasPrefix(
			bpfFilter.Name,
			"setera_",
		) {
			t.Fatalf(
				"Setera egress filter %q unexpectedly attached to %s",
				bpfFilter.Name,
				link.Attrs().Name,
			)
		}
	}
}

func requireNoSeteraFilters(
	t *testing.T,
	link netlink.Link,
) {
	t.Helper()

	for _, parent := range []uint32{
		netlink.HANDLE_MIN_INGRESS,
		netlink.HANDLE_MIN_EGRESS,
	} {
		filters, err := netlink.FilterList(
			link,
			parent,
		)
		if err != nil {
			t.Fatalf(
				"list filters on %s parent %#x: %v",
				link.Attrs().Name,
				parent,
				err,
			)
		}

		for _, filter := range filters {
			bpfFilter, ok :=
				filter.(*netlink.BpfFilter)

			if !ok {
				continue
			}

			if strings.HasPrefix(
				bpfFilter.Name,
				"setera_",
			) {
				t.Fatalf(
					"Setera filter %q still attached to %s",
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

func addIntegrationDummy(
	t *testing.T,
	name string,
) netlink.Link {
	t.Helper()

	link := &netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{
			Name: name,
		},
	}

	if err := netlink.LinkAdd(link); err != nil {
		t.Fatalf(
			"add dummy interface %s: %v",
			name,
			err,
		)
	}

	t.Cleanup(func() {
		_ = netlink.LinkDel(link)
	})

	created, err := netlink.LinkByName(name)
	if err != nil {
		t.Fatalf(
			"lookup created dummy interface %s: %v",
			name,
			err,
		)
	}

	if err := netlink.LinkSetUp(created); err != nil {
		t.Fatalf(
			"set dummy interface %s up: %v",
			name,
			err,
		)
	}

	return created
}

func requireMapID(
	t *testing.T,
	m *ebpf.Map,
) ebpf.MapID {
	t.Helper()

	if m == nil {
		t.Fatal("map is nil")
	}

	info, err := m.Info()
	if err != nil {
		t.Fatalf(
			"read map info: %v",
			err,
		)
	}

	id, ok := info.ID()
	if !ok {
		t.Fatal(
			"kernel did not expose map ID",
		)
	}

	return id
}
