package backend

import (
	"fmt"
	"net"
)

var _ device = (*base_device)(nil)

type base_device struct {
	name string
	ip   *net.IPNet
	mac  net.HardwareAddr
}

func (bd *base_device) GetName() string          { return bd.name }
func (bd *base_device) GetIP() *net.IPNet        { return bd.ip }
func (bd *base_device) GetMAC() net.HardwareAddr { return bd.mac }

func (bd *base_device) Update() error {
	// update device fields
	return nil
}

func (bd *base_device) Delete() error {
	// delete device from the host
	return fmt.Errorf("Delete method not implemented for %s", bd.name)
}

type vtep_device struct {
	base_device
	vni int
}
