package ebpfmanager

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

	"github/setera/pkg/ebpf"
)

// ReconcileRemotePods makes the remote portion of tc_podIDs match desired.
//
// This method is intended for daemon startup and full reconciliations. It
// enumerates the real shared BPF map so stale remote entries left by a crashed
// daemon can be removed even when Manager.remote starts empty.
//
// Local entries are never swept. A desired remote Pod that collides with an
// actual local BPF endpoint is rejected, even if local userspace recovery has
// not populated Manager.local yet.
func (m *Manager) ReconcileRemotePods(
	ctx context.Context,
	desired []RemotePod,
) error {
	desiredByIP, err := normalizeDesiredRemotePods(desired)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if m.deps.listEndpoints == nil {
		return fmt.Errorf("ebpfmanager: list endpoint dependency is nil")
	}

	actual, err := m.deps.listEndpoints()
	if err != nil {
		return fmt.Errorf("list Pod endpoints for remote reconciliation: %w", err)
	}

	actualByIP := make(map[netip.Addr]ebpf.PodEndpoint, len(actual))
	for _, endpoint := range actual {
		ip := endpoint.IP.Unmap()
		actualByIP[ip] = endpoint
	}

	desiredIPs := sortedRemotePodIPs(desiredByIP)

	// Validate every desired endpoint before changing the map.
	for _, ip := range desiredIPs {
		pod := desiredByIP[ip]

		if local, ok := m.local[ip]; ok {
			return fmt.Errorf(
				"%w: IP %s is already owned by local pod UID %q on %q",
				ErrRemotePodConflict,
				ip,
				local.pod.PodUID,
				local.pod.HostVethName,
			)
		}

		if endpoint, ok := actualByIP[ip]; ok &&
			endpoint.IfIndex != ebpf.RemotePodIfIndex {
			return fmt.Errorf(
				"%w: IP %s is an existing local BPF endpoint with ifindex %d",
				ErrRemotePodConflict,
				pod.IP,
				endpoint.IfIndex,
			)
		}
	}

	// Publish the complete desired set first. If this pass fails, already
	// successful entries are recorded in userspace and the next reconciliation
	// can continue safely.
	for _, ip := range desiredIPs {
		pod := desiredByIP[ip]

		if err := m.deps.upsertEndpoint(
			ip,
			pod.TenantID,
			ebpf.RemotePodIfIndex,
		); err != nil {
			return fmt.Errorf(
				"publish desired remote Pod ip=%s uid=%q tenant=%q: %w",
				ip,
				pod.PodUID,
				pod.TenantID,
				err,
			)
		}

		m.remote[ip] = remotePodRecord{
			PodUID:   pod.PodUID,
			TenantID: pod.TenantID,
		}
	}

	// Sweep only remote endpoints. This includes entries that exist solely in
	// the pinned map after a restart and stale userspace records from an
	// earlier full reconciliation.
	stale := make(map[netip.Addr]struct{})

	for ip, endpoint := range actualByIP {
		if endpoint.IfIndex != ebpf.RemotePodIfIndex {
			continue
		}
		if _, keep := desiredByIP[ip]; !keep {
			stale[ip] = struct{}{}
		}
	}

	for ip := range m.remote {
		if _, keep := desiredByIP[ip]; !keep {
			stale[ip] = struct{}{}
		}
	}

	staleIPs := make([]netip.Addr, 0, len(stale))
	for ip := range stale {
		staleIPs = append(staleIPs, ip)
	}
	sort.Slice(staleIPs, func(i, j int) bool {
		return staleIPs[i].Less(staleIPs[j])
	})

	for _, ip := range staleIPs {
		// Never delete an endpoint that the actual BPF snapshot identifies as
		// local, even if userspace somehow retained an obsolete remote record.
		if endpoint, ok := actualByIP[ip]; ok &&
			endpoint.IfIndex != ebpf.RemotePodIfIndex {
			delete(m.remote, ip)
			continue
		}

		if err := m.deps.deleteEndpoint(ip); err != nil {
			return fmt.Errorf(
				"delete stale remote Pod endpoint %s: %w",
				ip,
				err,
			)
		}
		delete(m.remote, ip)
	}

	return nil
}

func normalizeDesiredRemotePods(
	pods []RemotePod,
) (map[netip.Addr]RemotePod, error) {
	out := make(map[netip.Addr]RemotePod, len(pods))

	for _, pod := range pods {
		pod.IP = pod.IP.Unmap()

		if err := validateRemotePod(pod); err != nil {
			return nil, err
		}

		if existing, ok := out[pod.IP]; ok {
			if existing.PodUID == pod.PodUID &&
				existing.TenantID == pod.TenantID {
				continue
			}

			return nil, fmt.Errorf(
				"%w: desired remote IP %s belongs to both UID %q tenant %q and UID %q tenant %q",
				ErrRemotePodConflict,
				pod.IP,
				existing.PodUID,
				existing.TenantID,
				pod.PodUID,
				pod.TenantID,
			)
		}

		out[pod.IP] = pod
	}

	return out, nil
}

func sortedRemotePodIPs(
	pods map[netip.Addr]RemotePod,
) []netip.Addr {
	out := make([]netip.Addr, 0, len(pods))
	for ip := range pods {
		out = append(out, ip)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Less(out[j])
	})
	return out
}
