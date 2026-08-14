package noderouting

import "github/setera/pkg/network"

func fdbFor(ifIndex int, node RemoteNode) network.FDBEntry {
	return network.FDBEntry{
		IfIndex:  ifIndex,
		RemoteIP: node.UnderlayIP,
		MAC:      node.VTEPMAC,
	}
}

func fdbKey(entry network.FDBEntry) string {
	return entry.MAC.String() + "@" + entry.RemoteIP.Unmap().String()
}