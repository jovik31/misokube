package arp

import "net"

type ARPEntry struct {
	Device string
	IP     net.IP
	MAC    net.HardwareAddr
}

type ARPManager interface {
	Add(ne ARPEntry) error
	Update(ne ARPEntry) error
	Delete(ne ARPEntry) error
}

var DefaultARPManager ARPManager

func RegisterARPManager(mgr ARPManager) {
	if mgr == nil {
		panic("arp manager is nil")
	}
	DefaultARPManager = mgr
}

func Manager() ARPManager {
	return DefaultARPManager
}
