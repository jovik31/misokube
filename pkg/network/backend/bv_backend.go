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

func (bv *bv_backend) Create(tenantID string, subnet *net.IPNet, host string) error {

	br, err := bv.bridgeManager.Create(tenantID, subnet, host)
	if err != nil {
		return err
	}

	vt, err := bv.vtepManager.Create(tenantID, subnet, host)
	if err != nil {
		// If VTEP creation fails, we should clean up the bridge
		if delErr := bv.bridgeManager.Delete(br); delErr != nil {
			return delErr // return the original error if cleanup fails
		}
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
func (bv *bv_backend) Type() string                { return "bv_backend" }
func (bv *bv_backend) BridgeDevice() device.Device { return bv.Bridge }
func (bv *bv_backend) VtepDevice() device.Device   { return bv.VTEP }

// ---------------------------bridge manager-----------------------------

// base device
var _ device.Device = (*base_device)(nil)

type base_device struct {
	name string
	ip   *net.IPNet
	mac  net.HardwareAddr
}

func (bd *base_device) GetName() string          { return bd.name }
func (bd *base_device) GetIP() *net.IPNet        { return bd.ip }
func (bd *base_device) GetMAC() net.HardwareAddr { return bd.mac }

var _ device.DeviceManager = (*bridgeManager)(nil)

type bridgeManager struct{}

func (bm *bridgeManager) Create(tenantID string, subnet *net.IPNet, host string) (device.Device, error) {

	link, ip, err := bridge.SetupBridge(tenantID, subnet)
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
func (bm *bridgeManager) Update(dv device.Device, subnet *net.IPNet) error {

	// check if the device is a base_device
	err := bridge.UpdateBridgeIP(dv, subnet)
	if err != nil {
		return err
	}
	return nil
}
func (bm *bridgeManager) Delete(dv device.Device) error {

	//delete the device
	err := bridge.DeleteBridge(dv)
	if err != nil {
		return err
	}

	return nil
}
