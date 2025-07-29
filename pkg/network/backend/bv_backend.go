package backend

import "net"

type device interface {
	GetName() string          // get device name
	GetIP() *net.IPNet        // get device ip
	GetMAC() net.HardwareAddr // get name, ip and mac from device

	Update() error // update device fields
	Delete() error // delete device from the host
}

type deviceManager interface {
	Create(tenantID string, subnet *net.IPNet, host string) (device, error) // called to create the entire backend
	Delete(tenantID string) error                                           // called to delete the entire backend
}

var _ Backend = (*bv_backend)(nil)

type bv_backend struct {
	bridgeManager deviceManager
	vtepManager   deviceManager

	Bridge device
	VTEP   device
}

func (bv *bv_backend) Create(tenantID string, subnet *net.IPNet, host string) error { return nil }
func (bv *bv_backend) Update(subnet *net.IPNet, host string) error                  { return nil }
func (bv *bv_backend) Delete() error                                                { return nil }
func (bv *bv_backend) Type() string                                                 { return "bv_backend" }
