package podnetwork

import (
	"context"
	"errors"
	"fmt"

	"github/setera/pkg/network"
)

// RecoveryStats summarizes local Pod datapath recovery during daemon startup.
type RecoveryStats struct {
	Recovered   int
	MissingVeth int
}

// Recover rebuilds local Pod datapath ownership from durable IPAM allocations
// and the Linux kernel state that survived a daemon restart.
//
// Host-side veth names and ifindexes are deliberately not persisted. For every
// allocation, Recover rediscovers the existing host veth from the Pod /32
// route, then asks the datapath to reinstall/replace its eBPF state.
//
// A missing veth does not fail recovery. The allocation remains reserved in
// NodeIPAM so its address cannot be reused until a higher-level reconciliation
// can decide whether the allocation is stale.
func (c *Configurator) Recover(ctx context.Context) (RecoveryStats, error) {
	var stats RecoveryStats

	for _, allocation := range c.ipam.List() {
		if err := ctx.Err(); err != nil {
			return stats, err
		}

		veth, err := c.network.FindPodVeth(allocation.IP)
		if errors.Is(err, network.ErrPodVethNotFound) {
			stats.MissingVeth++
			continue
		}
		if err != nil {
			return stats, fmt.Errorf(
				"rediscover host veth for Pod IP %s: %w",
				allocation.IP,
				err,
			)
		}

		pod := LocalPod{
			IP:              allocation.IP,
			PodUID:          allocation.PodUID,
			TenantID:        allocation.TenantID,
			HostVethName:    veth.HostName,
			HostVethIfIndex: veth.HostIfIndex,
		}

		if err := c.datapath.RecoverLocalPod(ctx, pod); err != nil {
			return stats, fmt.Errorf(
				"recover local Pod IP %s UID %q: %w",
				allocation.IP,
				allocation.PodUID,
				err,
			)
		}

		stats.Recovered++
	}

	return stats, nil
}
