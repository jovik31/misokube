package backend

import (
	"net"

	"github/setera/pkg/network/device"
	"github/setera/pkg/network/device/vtep"
)

var _ Backend = (*v_backend)(nil)

type v_backend struct {
	vtepManager *vtep.Manager
	VTEP        device.Device
}

// NewVTEPBackend creates a backend that only provisions a VTEP device.
func NewVTEPBackend() Backend {
	return &v_backend{vtepManager: &vtep.Manager{}}
}

func (vb *v_backend) Create(tenantID string, subnet *net.IPNet, host string) error {
	vt, err := vb.vtepManager.Create(tenantID, subnet, host)
	if err != nil {
		return err
	}
	vb.VTEP = vt
	return nil
}

func (vb *v_backend) Update(subnet *net.IPNet, host string) error {
	_ = subnet
	_ = host
	// VTEP is node-wide; no per-tenant update required.
	return nil
}

func (vb *v_backend) Delete() error {
	// VTEP is shared across tenants; do not delete per-tenant.
	vb.VTEP = nil
	vb.vtepManager = nil
	return nil
}

func (vb *v_backend) Type() string { return "v_backend" }

func (vb *v_backend) Devices() []device.Device {
	if vb.VTEP == nil {
		return nil
	}
	return []device.Device{vb.VTEP}
}
