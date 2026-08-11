package noderouting

import "github/setera/pkg/network"

func neighborFor(ifIndex int, node RemoteNode) network.Neighbor {
	return network.Neighbor{
		IfIndex: ifIndex,
		IP:      node.VTEPIP,
		MAC:     node.VTEPMAC,
	}
}

func neighborKey(neighbor network.Neighbor) string {
	return neighbor.IP.Unmap().String()
}