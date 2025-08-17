package bridge

import (
	"github/setera/pkg/network/device"
	"net"
)

var _ device.DeviceManager = (*Manager)(nil)

type Manager struct{}

func NewDeviceManager() *Manager { return &Manager{} }

func (m *Manager) Create(tenantID string, subnet *net.IPNet, host string) (device.Device, error) {

	link, ip, err := SetupBridge(tenantID, subnet)
	if err != nil {
		return nil, err
	}
	return &Bridge{
		name: link.Attrs().Name,
		ip:   ip,
		mac:  link.Attrs().HardwareAddr,
	}, nil

}

func (m *Manager) Update(device device.Device, subnet *net.IPNet) error {
	//TODO implement me
	panic("implement me")
}

func (m *Manager) Delete(device device.Device) error {
	//TODO implement me
	panic("implement me")
}
