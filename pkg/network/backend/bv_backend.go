package backend

import (
	"net"
)

// bridge-vtep backend interface implementation
var _ Backend = (*bv_backend)(nil)

type bv_backend struct {
	bridgeManager deviceManager
	vtepManager   deviceManager

	Bridge device
	VTEP   device
}

func (bv *bv_backend) Create(tenantID string, subnet *net.IPNet, host string) error {

	bridge, err := bv.bridgeManager.Create(tenantID, subnet, host)
	if err != nil {
		return err
	}

	vtep, err := bv.vtepManager.Create(tenantID, subnet, host)

	bv.Bridge = bridge
	bv.VTEP = vtep

	return nil
}
func (bv *bv_backend) Update(subnet *net.IPNet, host string) error { return nil }
func (bv *bv_backend) Delete() error                               { return nil }
func (bv *bv_backend) Type() string                                { return "bv_backend" }

type deviceManager interface {
	Create(tenantID string, subnet *net.IPNet, host string) (device, error)
	Update(device device, subnet *net.IPNet) error // called to update a device
	Delete(device device) error                    // called to delete the entire backend
}

var _ deviceManager = (*bridgeManager)(nil)

type bridgeManager struct{}

func (bm *bridgeManager) Create(tenantID string, subnet *net.IPNet, host string) (device, error) {

	link, ip, err := SetupBridge(tenantID, subnet)
	if err != nil {
		return nil, err
	}

	bridge := &base_device{

		name: link.Attrs().Name,
		ip:   ip,
		mac:  link.Attrs().HardwareAddr,
	}
	return bridge, nil
}
func (bm *bridgeManager) Update(device device, subnet *net.IPNet) error { return nil }
func (bm *bridgeManager) Delete(device device) error                    { return nil }

var _ deviceManager = (*vtepManager)(nil)

type vtepManager struct{}

func (vm *vtepManager) Create(tenantID string, subnet *net.IPNet, host string) (device, error) {

	return nil, nil
}
func (vm *vtepManager) Update(device device, subnet *net.IPNet) error { return nil }
func (vm *vtepManager) Delete(device device) error                    { return nil }

type device interface {
	GetName() string          // get device name
	GetIP() *net.IPNet        // get device ip
	GetMAC() net.HardwareAddr // get name, ip and mac from device
}

var _ device = (*base_device)(nil)

type base_device struct {
	name string
	ip   *net.IPNet
	mac  net.HardwareAddr
}

func (bd *base_device) GetName() string          { return bd.name }
func (bd *base_device) GetIP() *net.IPNet        { return bd.ip }
func (bd *base_device) GetMAC() net.HardwareAddr { return bd.mac }

var _ device = (*vtep_device)(nil)

type vtep_device struct {
	base_device
	vni int
}

func (vd *vtep_device) GetVNI() int { return vd.vni }
