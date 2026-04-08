package subnet

import (
	"net"
)

type SubnetManager interface {
	// main requirements
	Allocate(id string) (*net.IPNet, error)
	Deallocate(id string) error
	Expand(id string) (*net.IPNet, error)
	Reduce(id string) (*net.IPNet, error)
	Release(id string) error

	// introspection
	Get(id string) (*net.IPNet, error)
	List() map[string]*net.IPNet
	FreeCount() int
	TotalCount() int
}

var DefaultSubnetManager SubnetManager

func RegisterSubnetManager(mgr SubnetManager) {
	if mgr == nil {
		panic("subnet manager is nil")
	}
	DefaultSubnetManager = mgr
}

func Manager() SubnetManager {
	return DefaultSubnetManager
}
