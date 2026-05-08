package daemon

import (
	"log"

	dps "github/setera/internal/dispatcher"
	"github/setera/internal/nmanager"
)

// Dispatcher is a narrow interface the daemon operator uses to forward
// tenant-level operations to a background dispatcher.
// Implementations should be non-blocking and idempotent.
type Dispatcher interface {
	EnsureTenant(namespace, name string)
	RemoveTenant(namespace, name string)
	EnsurePeer(remote nmanager.RemoteTenantInfra, tenantID string)
	RemovePeer(remote nmanager.RemoteTenantInfra, tenantID string)
	EnsurePodProg(tenantID string, podName string, ifName string)
	RemovePodProg(tenantID string, podName string)
	EnsureMap(tenantID string)
	RemoveMap(tenantID string)
	UpsertPodMap(tenantID string, podName string, podIP string, ifindex int, ifName string)
	DeletePodMap(podIP string)
}

// dpAdapter adapts the internal dispatcher to the Operator's narrow Dispatcher interface.
type dpAdapter struct{ d *dps.Dispatcher }

func (a dpAdapter) EnsureTenant(namespace, name string) {
	if a.d == nil || name == "" {
		return
	}
	log.Printf("daemon-dispatcher: EnsureTenant namespace=%s name=%s", namespace, name)
	a.d.Enqueue(dps.Command{TenantID: name, Op: dps.OpEnsure})
}

func (a dpAdapter) RemoveTenant(namespace, name string) {
	if a.d == nil || name == "" {
		return
	}
	log.Printf("daemon-dispatcher: RemoveTenant namespace=%s name=%s", namespace, name)
	a.d.Enqueue(dps.Command{TenantID: name, Op: dps.OpRemove})
}

func (a dpAdapter) EnsurePeer(remote nmanager.RemoteTenantInfra, tenantID string) {
	if a.d == nil || tenantID == "" {
		return
	}
	log.Printf("daemon-dispatcher: EnsurePeer tenantID=%s remote=%+v", tenantID, remote)
	a.d.Enqueue(dps.Command{TenantID: tenantID, Op: dps.OpEnsurePeer, Remote: remote})
}

func (a dpAdapter) RemovePeer(remote nmanager.RemoteTenantInfra, tenantID string) {
	if a.d == nil || tenantID == "" {
		return
	}
	log.Printf("daemon-dispatcher: RemovePeer tenantID=%s remote=%+v", tenantID, remote)
	a.d.Enqueue(dps.Command{TenantID: tenantID, Op: dps.OpRemovePeer, Remote: remote})
}

func (a dpAdapter) EnsurePodProg(tenantID string, podName string, ifName string) {
	if a.d == nil || tenantID == "" {
		return
	}
	log.Printf("daemon-dispatcher: EnsurePodProg tenantID=%s podName=%s", tenantID, podName)
	a.d.Enqueue(dps.Command{TenantID: tenantID, Op: dps.OpEnsurePodProg, PodName: podName, IfName: ifName})
}

func (a dpAdapter) RemovePodProg(tenantID string, podName string) {
	if a.d == nil || tenantID == "" {
		return
	}
	log.Printf("daemon-dispatcher: RemovePodProg tenantID=%s podName=%s", tenantID, podName)
	a.d.Enqueue(dps.Command{TenantID: tenantID, Op: dps.OpRemovePodProg, PodName: podName})
}

func (a dpAdapter) EnsureMap(tenantID string) {
	if a.d == nil || tenantID == "" {
		return
	}
	log.Printf("daemon-dispatcher: EnsureMap tenantID=%s", tenantID)
	a.d.Enqueue(dps.Command{TenantID: tenantID, Op: dps.OpEnsureMap})
}

func (a dpAdapter) RemoveMap(tenantID string) {
	if a.d == nil || tenantID == "" {
		return
	}
	log.Printf("daemon-dispatcher: RemoveMap tenantID=%s", tenantID)
	a.d.Enqueue(dps.Command{TenantID: tenantID, Op: dps.OpRemoveMap})
}

func (a dpAdapter) UpsertPodMap(tenantID string, podName string, podIP string, ifindex int, ifName string) {
	if a.d == nil || tenantID == "" || podIP == "" {
		return
	}
	log.Printf("daemon-dispatcher: UpsertPodMap tenantID=%s podName=%s podIP=%s ifindex=%d ifName=%s", tenantID, podName, podIP, ifindex, ifName)
	a.d.Enqueue(dps.Command{TenantID: tenantID, Op: dps.OpUpsertPodMap, PodName: podName, PodIP: podIP, Ifindex: ifindex, IfName: ifName})
}

func (a dpAdapter) DeletePodMap(podIP string) {
	if a.d == nil || podIP == "" {
		return
	}
	log.Printf("daemon-dispatcher: DeletePodMap podIP=%s", podIP)
	a.d.Enqueue(dps.Command{Op: dps.OpDeletePodMap, PodIP: podIP})
}

// NewDispatcherAdapter returns a Dispatcher interface backed by the internal dispatcher.
func NewDispatcherAdapter(d *dps.Dispatcher) Dispatcher { return dpAdapter{d: d} }
