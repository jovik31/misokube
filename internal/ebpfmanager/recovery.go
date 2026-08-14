package ebpfmanager

import (
	"context"
	"fmt"

	"github/setera/internal/podnetwork"
)

// RecoverLocalPod reinstalls the eBPF datapath for a local Pod discovered
// during daemon startup.
//
// Recovery deliberately uses the same installation path as a normal ADD.
// pkg/ebpf attaches TC programs with filter replacement, so stale Setera TC
// filters left by the previous daemon process are atomically replaced by the
// new program instance. The shared tc_podIDs entry is then refreshed and the
// new Manager rebuilds its in-memory ownership record.
//
// The caller is responsible for rediscovering HostVethName and HostVethIfIndex
// from Linux state before calling this method.
func (m *Manager) RecoverLocalPod(
	ctx context.Context,
	pod podnetwork.LocalPod,
) error {
	if err := m.AddLocalPod(ctx, pod); err != nil {
		return fmt.Errorf(
			"recover local Pod %s: %w",
			pod.IP,
			err,
		)
	}

	return nil
}
