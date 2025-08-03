package arp

import "net"

type ArpEntry struct {
	IP     net.IP
	Device string
	MAC    net.HardwareAddr
	Family int
}

type ArpManager interface {
	Add(ne ArpEntry) error
	Update(ne ArpEntry) error
	Delete(ne ArpEntry) error
}

var DefaultArpManager ArpManager

func SetDefaultArpManager(am ArpManager) {
	DefaultArpManager = am
}
