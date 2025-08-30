package vtep

import (
	"github/setera/pkg/network/device"
	"net"
)

var _ device.Device = (*VTEP)(nil)
var _ device.VTEPDevice = (*VTEP)(nil)

type VTEP struct {
	name string
	ip   *net.IPNet
	mac  net.HardwareAddr
	vni  int
}

func (v *VTEP) GetVNI() int              { return v.vni }
func (v *VTEP) GetName() string          { return v.name }
func (v *VTEP) GetIP() *net.IPNet        { return v.ip }
func (v *VTEP) GetMAC() net.HardwareAddr { return v.mac }
