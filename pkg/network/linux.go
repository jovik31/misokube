//go:build linux

package network

// Linux provides Linux network operations used by Setera components.
//
// The exported API uses standard-library types. Linux-specific dependencies,
// such as netlink and network namespace handles, stay inside this package.
type Linux struct{}

// NewLinux creates a Linux network implementation.
func NewLinux() *Linux {
	return &Linux{}
}

// Veth identifies the host side of a pod veth pair.
type Veth struct {
	HostName    string
	HostIfIndex int
}
