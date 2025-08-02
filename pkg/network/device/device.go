package device

import "net"

type Device interface {
	GetName() string
	GetIP() *net.IPNet
	GetMAC() net.HardwareAddr
}

type DeviceManager interface {
	Create(tenantID string, subnet *net.IPNet, host string) (Device, error)
	Update(device Device, subnet *net.IPNet) error
	Delete(device Device) error
}

var _ DeviceManager = (*bridgeManager)(nil)

type bridgeManager struct{}

func (bm *bridgeManager) Create(tenantID string, subnet *net.IPNet, host string) (Device, error) {

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
func (bm *bridgeManager) Update(device Device, subnet *net.IPNet) error { return nil }
func (bm *bridgeManager) Delete(device Device) error                    { return nil }

var _ DeviceManager = (*vtepManager)(nil)

type vtepManager struct{}

func (vm *vtepManager) Create(tenantID string, subnet *net.IPNet, host string) (Device, error) {

	link, ip, err := SetupVxlan(subnet, tenantID, host)
	if err != nil {
		return nil, err
	}

	vtep := &vtep_device{
		base_device: base_device{
			name: link.Attrs().Name,
			ip:   ip,
			mac:  link.Attrs().HardwareAddr,
		},
		vni: link.VxlanId,
	}

	return vtep, nil
}
func (vm *vtepManager) Update(device Device, subnet *net.IPNet) error { return nil }
func (vm *vtepManager) Delete(device Device) error                    { return nil }

var _ Device = (*base_device)(nil)

type base_device struct {
	name string
	ip   *net.IPNet
	mac  net.HardwareAddr
}

func (bd *base_device) GetName() string          { return bd.name }
func (bd *base_device) GetIP() *net.IPNet        { return bd.ip }
func (bd *base_device) GetMAC() net.HardwareAddr { return bd.mac }

var _ Device = (*vtep_device)(nil)

type vtep_device struct {
	base_device
	vni int
}

func (vd *vtep_device) GetVNI() int { return vd.vni }
