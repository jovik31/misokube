package nodeipam

import (
	"fmt"
	"net/netip"
)

// Owner identifies one CNI network attachment.
//
// ContainerID identifies the pod sandbox. IfName identifies the interface in
// that sandbox. The pair is stable across a repeated CNI ADD for the same
// attachment.
type Owner struct {
	ContainerID string
	IfName      string
}

// Request contains the metadata that NodeIPAM stores with an address.
type Request struct {
	Owner    Owner
	PodUID   string
	TenantID string
}

// Allocation is one live node-local IP allocation.
type Allocation struct {
	IP       netip.Addr
	Owner    Owner
	PodUID   string
	TenantID string
}

func (o Owner) validate() error {
	if o.ContainerID == "" {
		return fmt.Errorf("%w: container ID is empty", ErrInvalidOwner)
	}
	if o.IfName == "" {
		return fmt.Errorf("%w: interface name is empty", ErrInvalidOwner)
	}
	return nil
}

func (r Request) validate() error {
	if err := r.Owner.validate(); err != nil {
		return err
	}
	if r.PodUID == "" {
		return fmt.Errorf("%w: pod UID is empty", ErrInvalidRequest)
	}
	if r.TenantID == "" {
		return fmt.Errorf("%w: tenant ID is empty", ErrInvalidRequest)
	}
	return nil
}

func (a Allocation) validate() error {
	if !a.IP.IsValid() {
		return fmt.Errorf("%w: IP address is invalid", ErrCorruptState)
	}
	if err := a.Owner.validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptState, err)
	}
	if a.PodUID == "" {
		return fmt.Errorf("%w: pod UID is empty", ErrCorruptState)
	}
	if a.TenantID == "" {
		return fmt.Errorf("%w: tenant ID is empty", ErrCorruptState)
	}
	return nil
}

func (a Allocation) matches(r Request) bool {
	return a.Owner == r.Owner && a.PodUID == r.PodUID && a.TenantID == r.TenantID
}
