package daemon

import (
	dps "github/setera/internal/dispatcher"
)

// Dispatcher is a narrow interface the daemon operator uses to forward
// tenant-level operations to a background dispatcher.
// Implementations should be non-blocking and idempotent.
type Dispatcher interface {
	EnsureTenant(namespace, name string)
	RemoveTenant(namespace, name string)
}

// dpAdapter adapts the internal dispatcher to the Operator's narrow Dispatcher interface.
type dpAdapter struct{ d *dps.Dispatcher }

func (a dpAdapter) EnsureTenant(namespace, name string) {
	if a.d == nil || name == "" {
		return
	}
	a.d.Enqueue(dps.Command{TenantID: name, Op: dps.OpEnsure})
}

func (a dpAdapter) RemoveTenant(namespace, name string) {
	if a.d == nil || name == "" {
		return
	}
	a.d.Enqueue(dps.Command{TenantID: name, Op: dps.OpRemove})
}

// NewDispatcherAdapter returns a Dispatcher interface backed by the internal dispatcher.
func NewDispatcherAdapter(d *dps.Dispatcher) Dispatcher { return dpAdapter{d: d} }
