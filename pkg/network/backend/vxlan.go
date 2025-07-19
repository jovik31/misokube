package backend

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"log"
	"strings"

	config "github/setera/pkg"
	"github/setera/pkg/network/routing"
	"net"

	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
	// internal packages
)

func SetupVxlan() {}

func newVxlanDevice(vtepName string, vni int, vtepMac string) (*netlink.Vxlan, error) {

	mac, err := net.ParseMAC(vtepMac)
	if err != nil {
		return nil, err
	}

	gateway, gatewayIP, err := routing.GetDefaultGatewayInterface()
	if err != nil {
		return nil, errors.Wrap(err, "getSefaultDefaultGatewayInterface")
	}

	return ensureVxLan(&netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:         vtepName,
			HardwareAddr: mac,
			MTU:          gateway.MTU - config.EncapOverhead,
		},
		VxlanId:      vni,
		VtepDevIndex: gateway.Index,
		SrcAddr:      gatewayIP,
		Port:         config.VxlanPort,
		Learning:     false,
	}) // Default VXLAN MTU

}

func ensureVxLan(vxlan *netlink.Vxlan) (*netlink.Vxlan, error) {
	link, err := netlink.LinkByName(vxlan.Name)
	if err == nil {
		v, ok := link.(*netlink.Vxlan)
		if !ok {
			return nil, errors.Errorf("link %s already exists but not vxlan device", vxlan.Name)
		}

		log.Printf("vxlan device %s already exists", vxlan.Name)
		return v, nil
	}

	if !strings.Contains(err.Error(), "Link not found") {
		return nil, errors.Wrapf(err, "get link %s error", vxlan.Name)
	}

	log.Printf("vxlan device %s not found, and create it", vxlan.Name)

	if err = netlink.LinkAdd(vxlan); err != nil {
		return nil, errors.Wrap(err, "LinkAdd error")
	}

	link, err = netlink.LinkByName(vxlan.Name)
	if err != nil {
		return nil, errors.Wrap(err, "LinkByName error")
	}

	return link.(*netlink.Vxlan), nil
}

func VtepMAC(tenantID, nodeName string) net.HardwareAddr {
	key := tenantID + "|" + nodeName
	sum := sha1.Sum([]byte(key)) // 20 bytes
	mac := make([]byte, 6)
	copy(mac, sum[:6])
	mac[0] = (mac[0] & 0xFE) | 0x02 // local bit set, multicast bit cleared
	return net.HardwareAddr(mac)
}

func VNI(tenant string) int {
	sum := sha256.Sum256([]byte(tenant))
	// Take first 4 bytes as big-endian uint32
	val := binary.BigEndian.Uint32(sum[0:4]) // 32 bits
	// Fold to 24 bits
	vni := val & 0xFFFFFF
	if vni == 0 {
		vni = 1
	}
	return int(vni)
}
