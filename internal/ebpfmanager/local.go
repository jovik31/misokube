package ebpfmanager

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github/setera/internal/podnetwork"
)

// AddLocalPod installs the Setera datapath for one local Pod.
//
// The TC program is attached before the Pod is published in tc_podIDs. If the
// map update fails, the program attachment is rolled back.
func (m *Manager) AddLocalPod(ctx context.Context, pod podnetwork.LocalPod) error {
	pod.IP = pod.IP.Unmap()
	if err := validateLocalPod(pod); err != nil {
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

	if existing, ok := m.local[pod.IP]; ok {
		if !sameLocalPod(existing.pod, pod) {
			return fmt.Errorf(
				"%w: IP %s is already owned by pod UID %q on %q",
				ErrLocalPodConflict,
				pod.IP,
				existing.pod.PodUID,
				existing.pod.HostVethName,
			)
		}

		// Duplicate CNI ADD is idempotent. Refreshing the shared endpoint map is
		// cheap and repairs the map if it was externally removed.
		if err := m.deps.upsertEndpoint(
			pod.IP,
			pod.TenantID,
			pod.HostVethIfIndex,
		); err != nil {
			return fmt.Errorf("refresh local Pod endpoint %s: %w", pod.IP, err)
		}
		return nil
	}

	program, err := m.deps.attachPodProgram(
		pod.HostVethName,
		pod.TenantID,
	)
	if err != nil {
		return fmt.Errorf(
			"attach local Pod program ip=%s if=%q tenant=%q: %w",
			pod.IP,
			pod.HostVethName,
			pod.TenantID,
			err,
		)
	}

	if err := m.deps.upsertEndpoint(
		pod.IP,
		pod.TenantID,
		pod.HostVethIfIndex,
	); err != nil {
		rollbackErr := program.Close()
		return errors.Join(
			fmt.Errorf("publish local Pod endpoint %s: %w", pod.IP, err),
			wrapRollbackError("detach local Pod program", rollbackErr),
		)
	}

	m.local[pod.IP] = localPodState{
		pod: localPodRecord{
			PodUID:          pod.PodUID,
			TenantID:        pod.TenantID,
			HostVethName:    pod.HostVethName,
			HostVethIfIndex: pod.HostVethIfIndex,
		},
		program: program,
	}

	return nil
}

// DeleteLocalPod removes one local Pod from the Setera datapath.
//
// The shared endpoint-map entry is removed before the TC program is detached,
// so new traffic cannot be redirected to an endpoint while it is being torn
// down. A missing in-memory record is valid after a daemon restart: deleting
// the map entry is still useful and the veth deletion performed by podnetwork
// removes any TC filters that remain attached to that interface.
func (m *Manager) DeleteLocalPod(ctx context.Context, ip netip.Addr) error {
	ip = ip.Unmap()
	if err := validateLocalPodIP(ip); err != nil {
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

	state, tracked := m.local[ip]

	if err := m.deps.deleteEndpoint(ip); err != nil {
		return fmt.Errorf("delete local Pod endpoint %s: %w", ip, err)
	}

	if !tracked {
		return nil
	}

	closeErr := state.program.Close()

	// Once the endpoint map entry is gone, this manager no longer considers
	// the Pod reachable. Remove the in-memory record even if detaching the TC
	// program reports an error. podnetwork will still delete the veth, which
	// removes any filter left on that interface, and a later CNI DEL can then
	// complete idempotently instead of being stuck on a one-shot Close error.
	delete(m.local, ip)

	if closeErr != nil {
		return fmt.Errorf("detach local Pod program %s: %w", ip, closeErr)
	}

	return nil
}

func validateLocalPod(pod podnetwork.LocalPod) error {
	if err := validateLocalPodIP(pod.IP); err != nil {
		return err
	}
	if strings.TrimSpace(pod.PodUID) == "" {
		return fmt.Errorf("%w: pod UID is empty", ErrInvalidLocalPod)
	}
	if strings.TrimSpace(pod.TenantID) == "" {
		return fmt.Errorf("%w: tenant ID is empty", ErrInvalidLocalPod)
	}
	if strings.TrimSpace(pod.HostVethName) == "" {
		return fmt.Errorf("%w: host veth name is empty", ErrInvalidLocalPod)
	}
	if pod.HostVethIfIndex <= 0 {
		return fmt.Errorf(
			"%w: invalid host veth ifindex %d",
			ErrInvalidLocalPod,
			pod.HostVethIfIndex,
		)
	}
	return nil
}

func validateLocalPodIP(ip netip.Addr) error {
	if !ip.IsValid() || !ip.Is4() || ip.Zone() != "" {
		return fmt.Errorf("%w: invalid IPv4 Pod address %s", ErrInvalidLocalPod, ip)
	}
	return nil
}

func sameLocalPod(record localPodRecord, pod podnetwork.LocalPod) bool {
	return record.PodUID == pod.PodUID &&
		record.TenantID == pod.TenantID &&
		record.HostVethName == pod.HostVethName &&
		record.HostVethIfIndex == pod.HostVethIfIndex
}

func wrapRollbackError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
