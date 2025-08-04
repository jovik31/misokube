package fdb

import (
	"net"
)

type FDBEntry struct {
	Device string
	Family int
	State  int
	Flags  int
	IP     net.IP
	Mac    net.HardwareAddr
}

type FDBManager interface {
	Add(fe FDBEntry) error
	Update(fe FDBEntry) error
	Delete(fe FDBEntry) error
}

var DefaultFDBManager FDBManager

func RegisterFDBManager(mgr FDBManager) {

	if mgr == nil {
		panic("FDBManager is nil")
	}
	DefaultFDBManager = mgr
}

func Manager() FDBManager {
	return DefaultFDBManager
}
