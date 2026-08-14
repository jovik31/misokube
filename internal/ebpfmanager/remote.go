package ebpfmanager

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github/setera/pkg/ebpf"
)

// RemotePod is the userspace identity needed to manage one Pod that lives on
// another Node.
//
// PodUID is intentionally retained in manager state even though it is not
// stored in the BPF map. It lets the manager ignore stale delete events after
// Kubernetes reuses a Pod IP for a newer Pod.
type RemotePod struct {
	IP       netip.Addr
	PodUID   string
	TenantID string
}

// UpsertRemotePod creates or refreshes the shared tc_podIDs entry for a remote
// Pod. Remote endpoints never receive a local TC program; their endpoint-map
// value uses ebpf.RemotePodIfIndex.
//
// Reusing the same remote IP for a newer Pod is allowed. The new Pod UID
// replaces the old userspace ownership record only after the BPF map update
// succeeds.
func (m *Manager) UpsertRemotePod(
	ctx context.Context,
	pod RemotePod,
) error {
	pod.IP = pod.IP.Unmap()
	if err := validateRemotePod(pod); err != nil {
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

	if local, ok := m.local[pod.IP]; ok {
		return fmt.Errorf(
			"%w: IP %s is already owned by local pod UID %q on %q",
			ErrRemotePodConflict,
			pod.IP,
			local.pod.PodUID,
			local.pod.HostVethName,
		)
	}

	if err := m.deps.upsertEndpoint(
		pod.IP,
		pod.TenantID,
		ebpf.RemotePodIfIndex,
	); err != nil {
		return fmt.Errorf(
			"publish remote Pod endpoint ip=%s uid=%q tenant=%q: %w",
			pod.IP,
			pod.PodUID,
			pod.TenantID,
			err,
		)
	}

	m.remote[pod.IP] = remotePodRecord{
		PodUID:   pod.PodUID,
		TenantID: pod.TenantID,
	}

	return nil
}

// DeleteRemotePod removes a remote Pod from the shared endpoint map.
//
// podUID guards against stale Kubernetes delete events. If the IP is already
// tracked by a newer remote Pod UID, the stale delete is ignored.
//
// If no remote record exists, deletion still reaches the BPF map. That makes
// the operation useful after a daemon restart, when the pinned map can contain
// a stale remote entry while userspace state is empty.
//
// A local owner always wins: a remote delete is never allowed to remove a
// tc_podIDs entry that is currently tracked as local.
func (m *Manager) DeleteRemotePod(
	ctx context.Context,
	ip netip.Addr,
	podUID string,
) error {
	ip = ip.Unmap()
	if err := validateRemotePodDelete(ip, podUID); err != nil {
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

	if local, ok := m.local[ip]; ok {
		return fmt.Errorf(
			"%w: IP %s is currently owned by local pod UID %q on %q",
			ErrRemotePodConflict,
			ip,
			local.pod.PodUID,
			local.pod.HostVethName,
		)
	}

	if existing, ok := m.remote[ip]; ok &&
		existing.PodUID != podUID {
		// Stale delete for an older Pod that used the same IP. The current
		// endpoint belongs to a newer Pod and must remain untouched.
		return nil
	}

	if err := m.deps.deleteEndpoint(ip); err != nil {
		return fmt.Errorf(
			"delete remote Pod endpoint ip=%s uid=%q: %w",
			ip,
			podUID,
			err,
		)
	}

	delete(m.remote, ip)
	return nil
}

func validateRemotePod(pod RemotePod) error {
	if err := validateRemotePodIP(pod.IP); err != nil {
		return err
	}
	if strings.TrimSpace(pod.PodUID) == "" {
		return fmt.Errorf(
			"%w: pod UID is empty",
			ErrInvalidRemotePod,
		)
	}
	if strings.TrimSpace(pod.TenantID) == "" {
		return fmt.Errorf(
			"%w: tenant ID is empty",
			ErrInvalidRemotePod,
		)
	}
	return nil
}

func validateRemotePodDelete(
	ip netip.Addr,
	podUID string,
) error {
	if err := validateRemotePodIP(ip); err != nil {
		return err
	}
	if strings.TrimSpace(podUID) == "" {
		return fmt.Errorf(
			"%w: pod UID is empty",
			ErrInvalidRemotePod,
		)
	}
	return nil
}

func validateRemotePodIP(ip netip.Addr) error {
	if !ip.IsValid() || !ip.Is4() || ip.Zone() != "" {
		return fmt.Errorf(
			"%w: invalid IPv4 Pod address %s",
			ErrInvalidRemotePod,
			ip,
		)
	}
	return nil
}
