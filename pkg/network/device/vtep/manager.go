package vtep

import (
	"github/setera/pkg/network/device"
	"net"
)

var _ device.DeviceManager = (*Manager)(nil)

type Manager struct{}

func (m Manager) Create(tenantID string, subnet *net.IPNet, host string) (device.Device, error) {
	//TODO implement me
	panic("implement me")
}

func (m Manager) Update(device device.Device, subnet *net.IPNet) error {
	//TODO implement me
	panic("implement me")
}

func (m Manager) Delete(device device.Device) error {
	//TODO implement me
	panic("implement me")
}
