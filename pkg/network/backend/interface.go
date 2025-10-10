package backend

import (
	"net"

	"github/setera/pkg/network/device"
)

type Backend interface {
	Create(tenantID string, subnet *net.IPNet, nodeName string) error
	Update(subnet *net.IPNet, nodeName string) error
	Delete() error
	Type() string
	Devices() []device.Device
}
