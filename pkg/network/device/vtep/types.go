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

func (V VTEP) GetVNI() int {
	//TODO implement me
	panic("implement me")
}

func (V VTEP) GetName() string {
	//TODO implement me
	panic("implement me")
}

func (V VTEP) GetIP() *net.IPNet {
	//TODO implement me
	panic("implement me")
}

func (V VTEP) GetMAC() net.HardwareAddr {
	//TODO implement me
	panic("implement me")
}
