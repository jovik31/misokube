package device

import (
	"net"
)

type Device interface {
	GetName() string
	GetIP() *net.IPNet
	GetMAC() net.HardwareAddr
}

type VTEPDevice interface {
	Device
	GetVNI() int
}

type DeviceManager interface {
	Create(tenantID string, subnet *net.IPNet, args ...string) (Device, error)
	Update(device Device, subnet *net.IPNet) error
	Delete(device Device) error
}
