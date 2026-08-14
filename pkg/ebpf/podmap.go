package ebpf

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github/setera/pkg/ebpf/loader"
)

const (
	// RemotePodIfIndex marks a Pod endpoint that lives on another Node.
	//
	// The BPF value is stored as an unsigned 32-bit integer, so -1 becomes
	// UINT32_MAX, matching the sentinel used by tc_router.c and node_router.c.
	RemotePodIfIndex = -1

	maxPodMapTenantIDBytes = 63
)

// UpsertPodEndpoint creates or replaces the tc_podIDs entry for a Pod.
//
// Local Pods must use their positive host-side veth ifindex.
// Remote Pods must use RemotePodIfIndex.
func UpsertPodEndpoint(
	ip netip.Addr,
	tenant string,
	ifIndex int,
) error {
	return upsertPodEndpoint(
		ip,
		tenant,
		ifIndex,
		loader.WritePodTenantVeth,
	)
}

// DeletePodEndpoint removes a Pod from tc_podIDs.
//
// Deleting a missing entry is idempotent.
func DeletePodEndpoint(ip netip.Addr) error {
	return deletePodEndpoint(
		ip,
		loader.DeletePodTenantVeth,
	)
}

type podEndpointWriter func(
	net.IP,
	string,
	int,
) error

type podEndpointDeleter func(net.IP) error

func upsertPodEndpoint(
	ip netip.Addr,
	tenant string,
	ifIndex int,
	write podEndpointWriter,
) error {
	ip = ip.Unmap()

	if !ip.IsValid() || !ip.Is4() {
		return fmt.Errorf(
			"ebpf: invalid Pod IPv4 address %s",
			ip,
		)
	}

	if strings.TrimSpace(tenant) == "" {
		return fmt.Errorf("ebpf: Pod tenant is empty")
	}

	if len(tenant) > maxPodMapTenantIDBytes {
		return fmt.Errorf(
			"ebpf: Pod tenant %q is too long: got %d bytes, maximum is %d",
			tenant,
			len(tenant),
			maxPodMapTenantIDBytes,
		)
	}

	if ifIndex != RemotePodIfIndex && ifIndex <= 0 {
		return fmt.Errorf(
			"ebpf: invalid Pod ifindex %d: use a positive local host-veth ifindex or RemotePodIfIndex",
			ifIndex,
		)
	}

	if write == nil {
		return fmt.Errorf("ebpf: Pod endpoint writer is nil")
	}

	if err := write(
		net.IP(ip.AsSlice()),
		tenant,
		ifIndex,
	); err != nil {
		return fmt.Errorf(
			"ebpf: upsert Pod endpoint ip=%s tenant=%q ifindex=%d: %w",
			ip,
			tenant,
			ifIndex,
			err,
		)
	}

	return nil
}

func deletePodEndpoint(
	ip netip.Addr,
	deleteFn podEndpointDeleter,
) error {
	ip = ip.Unmap()

	if !ip.IsValid() || !ip.Is4() {
		return fmt.Errorf(
			"ebpf: invalid Pod IPv4 address %s",
			ip,
		)
	}

	if deleteFn == nil {
		return fmt.Errorf("ebpf: Pod endpoint deleter is nil")
	}

	if err := deleteFn(net.IP(ip.AsSlice())); err != nil {
		return fmt.Errorf(
			"ebpf: delete Pod endpoint ip=%s: %w",
			ip,
			err,
		)
	}

	return nil
}
