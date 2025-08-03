package fdb

import (
	"net"
)

type FDBEntry struct {
	Mac    net.HardwareAddr
	Device string
	Port   int
	State  int
}

type FDBManager interface {
	Add(fe FDBEntry) error
	Update(fe FDBEntry) error
	Delete(fe FDBEntry) error
}
