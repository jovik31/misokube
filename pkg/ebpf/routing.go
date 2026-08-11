package ebpf

import (
	"fmt"

	"github/setera/pkg/ebpf/loader"
)

// SetVXLANIfIndex publishes the node-wide VXLAN interface used by Pod TC
// programs when a destination endpoint is remote.
func SetVXLANIfIndex(ifIndex int) error {
	return setVXLANIfIndex(ifIndex, loader.WriteVXLANIfIndex)
}

type vxlanIfIndexWriter func(int) error

func setVXLANIfIndex(ifIndex int, write vxlanIfIndexWriter) error {
	if ifIndex <= 0 {
		return fmt.Errorf("ebpf: invalid VXLAN ifindex %d", ifIndex)
	}
	if write == nil {
		return fmt.Errorf("ebpf: VXLAN ifindex writer is nil")
	}
	if err := write(ifIndex); err != nil {
		return fmt.Errorf("ebpf: publish VXLAN ifindex %d: %w", ifIndex, err)
	}
	return nil
}