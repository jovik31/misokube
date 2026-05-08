package ebpfmanager

import (
	"fmt"
	"net"

	"github/setera/pkg/network/policy"
	_ "github/setera/pkg/network/policy/pod"
	_ "github/setera/pkg/network/policy/tenant"
)

func NewEbpfManager(rootCIDR *net.IPNet, nodeName string) (*EbpfManagerImpl, error) {
	return NewEbpfManagerWithDeps(rootCIDR, nodeName, Deps{})
}

func NewEbpfManagerWithDeps(rootCIDR *net.IPNet, nodeName string, d Deps) (*EbpfManagerImpl, error) {
	if d.TenantPolicy == nil {
		d.TenantPolicy = policy.Manager()
	}
	if d.PodPolicy == nil {
		d.PodPolicy = policy.PodManager()
	}
	if d.PodPolicy == nil {
		return nil, fmt.Errorf("pod policy manager not registered")
	}

	em := &EbpfManagerImpl{
		RootCIDR:      rootCIDR,
		NodeName:      nodeName,
		TenantRecords: make(map[string]*TenantRecord),
		TP:            d.TenantPolicy,
		PP:            d.PodPolicy,
	}
	em.defaultPodIfName = func(_ string, podName string) string { return podName }

	return em, nil
}
