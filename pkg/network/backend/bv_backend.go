package backend

import (
	"github/setera/pkg/network/device"
	"github/setera/pkg/network/device/bridge"
	"github/setera/pkg/network/device/vtep"
	"net"
)

// bridge-vtep backend interface implementation
var _ Backend = (*bv_backend)(nil)

type bv_backend struct {
	bridgeManager *bridge.Manager // manages the bridge device
	vtepManager   *vtep.Manager   // manages the VTEP device

	Bridge device.Device // Bridge device
	VTEP   device.Device // VTEP device
}

// NewBridgeVTEPBackend returns the default bridge+vtep backend implementation.
// It wires default device managers and returns a Backend ready for Create/Update/Delete.
func NewBridgeVTEPBackend() Backend {
	return &bv_backend{
		bridgeManager: bridge.NewDeviceManager(),
		vtepManager:   &vtep.Manager{},
	}
}

func (bv *bv_backend) Create(tenantID string, subnet *net.IPNet, host string) error {

	br, err := bv.bridgeManager.Create(tenantID, subnet)
	if err != nil {
		return err
	}

	vt, err := bv.vtepManager.Create(tenantID, subnet, host) // pass node name as arg
	if err != nil {
		// If VTEP creation fails, we should clean up the bridge
		if delErr := bv.bridgeManager.Delete(br); delErr != nil {
			return delErr // return the original error if cleanup fails
		}
		return err
	}

	bv.Bridge = br
	bv.VTEP = vt

	return nil
}
func (bv *bv_backend) Update(subnet *net.IPNet, host string) error {

	if err := bv.bridgeManager.Update(bv.Bridge, subnet); err != nil {
		return err
	}
	if err := bv.vtepManager.Update(bv.VTEP, subnet); err != nil {
		return err
	}

	return nil
}
func (bv *bv_backend) Delete() error {

	if err := bv.vtepManager.Delete(bv.VTEP); err != nil {
		return err
	}
	if err := bv.bridgeManager.Delete(bv.Bridge); err != nil {
		return err
	}
	bv.Bridge = nil
	bv.VTEP = nil

	// delete the managers
	bv.bridgeManager = nil
	bv.vtepManager = nil

	return nil
}
func (bv *bv_backend) Type() string { return "bv_backend" }

// Devices returns the set of devices managed by this backend.
func (bv *bv_backend) Devices() []device.Device {
	var devs []device.Device
	if bv.Bridge != nil {
		devs = append(devs, bv.Bridge)
	}
	if bv.VTEP != nil {
		devs = append(devs, bv.VTEP)
	}
	return devs
}

// VTEP returns the VTEP device from a Backend, if present.
func VTEP(b Backend) (device.Device, bool) {
	for _, d := range b.Devices() {
		// Prefer a typed check if you have one (e.g., vtep.Is(d) or device.KindVTEP)
		if d.Type() == "vtep" {
			return d, true
		}
	}
	return nil, false
}

// Bridge returns the bridge device from a Backend, if present.
func Bridge(b Backend) (device.Device, bool) {
	for _, d := range b.Devices() {
		if d.Type() == "bridge" {
			return d, true
		}
	}
	return nil, false
}
