package vtep

import (
	"fmt"
	"github/setera/pkg/network/device"
	"net"
)

var _ device.DeviceManager = (*Manager)(nil)

type Manager struct{}

func (m Manager) Create(tenantID string, subnet *net.IPNet, args ...string) (device.Device, error) {

	if len(args) < 1 || args[0] == "" {
		return nil, fmt.Errorf("vtep create: node name required")
	}
	nodeName := args[0]
	link, ip, err := SetupVxlan(subnet, nodeName)
	if err != nil {
		return nil, err
	}
	return &VTEP{
		name: link.Attrs().Name,
		ip:   ip,
		mac:  link.Attrs().HardwareAddr,
		vni:  link.VxlanId,
	}, nil
}

func (m Manager) Update(device device.Device, subnet *net.IPNet) error {

	_, err := UpdateIP(device, subnet)
	if err != nil {
		return err
	}
	return nil

}

func (m Manager) Delete(device device.Device) error {

	err := DeleteVTEP(device)
	if err != nil {
		return err
	}
	return nil
}
