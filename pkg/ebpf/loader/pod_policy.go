package loader

import (
	"errors"
	"fmt"
	"sync"

	"github.com/vishvananda/netlink"
)

// PodPolicy owns the ingress and egress TC filters for one Pod host veth.
//
// Ingress checks traffic sent by the Pod. Egress checks traffic delivered to
// the Pod after Linux routing and Service translation.
type PodPolicy struct {
	firewall     *TCFirewall
	egressFilter *netlink.BpfFilter

	closeOnce sync.Once
	closeErr  error
}

// NewPodPolicy attaches Setera tenant policy to both TC directions of one Pod
// host veth.
func NewPodPolicy(
	ifaceName string,
	tenantName string,
) (*PodPolicy, error) {
	firewall, err := NewTCFirewall(
		ifaceName,
		tenantName,
	)
	if err != nil {
		return nil, err
	}

	egressFilter := newTCBpfFilter(
		firewall.ingressFilter.Attrs().LinkIndex,
		netlink.HANDLE_MIN_EGRESS,
		firewall.objs.TcFirewallEgress.FD(),
		"setera_tc_egress",
	)

	if err := netlink.FilterReplace(egressFilter); err != nil {
		closeErr := firewall.Close()

		return nil, errors.Join(
			fmt.Errorf("attach Pod TC egress: %w", err),
			wrapTCleanupError("rollback Pod TC ingress", closeErr),
		)
	}

	return &PodPolicy{
		firewall:     firewall,
		egressFilter: egressFilter,
	}, nil
}

// Close removes both Pod TC filters. It keeps the shared clsact qdisc.
func (p *PodPolicy) Close() error {
	if p == nil {
		return nil
	}

	p.closeOnce.Do(func() {
		p.closeErr = errors.Join(
			wrapTCleanupError(
				"delete Pod TC egress filter",
				deleteTCFilter(
					p.egressFilter,
					netlink.FilterDel,
				),
			),
			wrapTCleanupError(
				"close Pod TC ingress",
				p.firewall.Close(),
			),
		)
	})

	return p.closeErr
}
