package ebpf

import (
	"fmt"
	"net/netip"
	"sort"

	"github/setera/pkg/ebpf/loader"
)

// PodEndpoint is one entry in Setera's shared tc_podIDs map.
//
// Positive IfIndex values identify local host-side veths.
// RemotePodIfIndex identifies Pods that live on another Node.
type PodEndpoint struct {
	IP       netip.Addr
	TenantID string
	IfIndex  int
}

// ListPodEndpoints returns a snapshot of the shared Pod endpoint map.
func ListPodEndpoints() ([]PodEndpoint, error) {
	entries, err := loader.ListPodTenantVeth()
	if err != nil {
		return nil, fmt.Errorf("ebpf: list Pod endpoints: %w", err)
	}

	return podEndpointsFromLoader(entries)
}

func podEndpointsFromLoader(
	entries []loader.PodTenantVeth,
) ([]PodEndpoint, error) {
	out := make([]PodEndpoint, 0, len(entries))
	for _, entry := range entries {
		addr, ok := netip.AddrFromSlice(entry.IP)
		if !ok {
			return nil, fmt.Errorf(
				"ebpf: invalid Pod endpoint IP %v",
				entry.IP,
			)
		}

		addr = addr.Unmap()
		if !addr.Is4() {
			return nil, fmt.Errorf(
				"ebpf: non-IPv4 Pod endpoint %s",
				addr,
			)
		}

		if entry.IfIndex != RemotePodIfIndex && entry.IfIndex <= 0 {
			return nil, fmt.Errorf(
				"ebpf: invalid Pod endpoint ifindex %d for %s",
				entry.IfIndex,
				addr,
			)
		}

		out = append(out, PodEndpoint{
			IP:       addr,
			TenantID: entry.Tenant,
			IfIndex:  entry.IfIndex,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].IP.Less(out[j].IP)
	})

	return out, nil
}
