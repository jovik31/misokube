package backend

import "net"

type Backend interface {
	Create(tenantID string, subnet *net.IPNet, nodeName string) error
	Update(subnet *net.IPNet, nodeName string) error
	Delete() error
	Type() string
}
