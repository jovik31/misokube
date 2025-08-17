package bridge

import (
	"github/setera/pkg/network/device"
	"net"
)

var _ device.Device = (*Bridge)(nil)

type Bridge struct {
	name string
	ip   *net.IPNet
	mac  net.HardwareAddr
}

func (b *Bridge) GetName() string          { return b.name }
func (b *Bridge) GetIP() *net.IPNet        { return b.ip }
func (b *Bridge) GetMAC() net.HardwareAddr { return b.mac }
