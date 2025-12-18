package bridge

import (
	devpkg "github/setera/pkg/network/device"
	"net"
)

var _ devpkg.DeviceManager = (*Manager)(nil)

type Manager struct{}

func NewDeviceManager() *Manager { return &Manager{} }

func (m *Manager) Create(tenantID string, subnet *net.IPNet, args ...string) (devpkg.Device, error) {

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

func (m *Manager) Update(dev devpkg.Device, subnet *net.IPNet) error {

	if err := UpdateBridgeIP(dev, subnet); err != nil {
		return err
	}
	if br, ok := dev.(*Bridge); ok {
		if ipNet, err := devpkg.FirstIP(subnet); err == nil {
			br.ip = ipNet
		}
	}
	return nil
}

func (m *Manager) Delete(dev devpkg.Device) error {

	err := DeleteBridge(dev)
	if err != nil {
		return err
	}
	return nil
}
