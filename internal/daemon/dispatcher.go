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

// NewDispatcherAdapter returns a Dispatcher interface backed by the internal dispatcher.
func NewDispatcherAdapter(d *dps.Dispatcher) Dispatcher { return dpAdapter{d: d} }
