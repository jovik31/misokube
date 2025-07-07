package network

import "net"

type SubnetRecord struct {
	Network *net.IPNet
	Pods    []*PodRecord
	Bridge  *DeviceRecord
	VTEP    *DeviceRecord
}

// Stores network device data
type DeviceRecord struct {
	Name string
	IP   string
	MAC  string
}

// Stores Pod data
type PodRecord struct {
	Name  string
	NetNS string
	IP    string
}

type ContainerNetInfo struct {
	IP        int
	DefineAll string
	Statue    *PodRecord
	Define    string
}
