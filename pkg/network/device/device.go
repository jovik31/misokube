package device

import (
	"net"
)

// DeviceType identifies the concrete kind of device implementation.
type DeviceType string

const (
	TypeBridge DeviceType = "bridge"
	TypeVTEP   DeviceType = "vtep"
)

type Device interface {
	GetName() string
	GetIP() *net.IPNet
	GetMAC() net.HardwareAddr
	// Type returns the concrete device kind (e.g., bridge, vtep).
	Type() DeviceType
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
