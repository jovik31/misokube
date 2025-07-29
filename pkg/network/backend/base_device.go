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

func (bd *base_device) Get() (device_name string, device_ip string, device_mac string, err error) {

	if bd.name == "" || bd.ip == nil || bd.mac == nil {
		return "", "", "", fmt.Errorf("failed to get device data")
	}

	return bd.name, bd.ip.String(), bd.mac.String(), nil
}

func (bd *base_device) Update() error {

	// update ip of device
	return nil
}

func (bd *base_device) Delete() error {

	return nil
}
