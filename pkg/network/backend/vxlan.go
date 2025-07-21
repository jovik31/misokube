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

func SetupVxlan(subnet *net.IPNet, id string, nodeName string) (*netlink.Vxlan, *net.IPNet, error) {

	// generate VTEP name
	vtepName, err := GenerateDeviceName(config.VxlanPrefix, id)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "generate VTEP name for %s", id)
	}

	// generate VTEP MAC address
	vtepMac := VtepMAC(id, nodeName).String()
	// generate VNI
	vni := VNI(id)

	vxlanLink, err := newVxlanDevice(vtepName, vni, vtepMac)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "create vxlan device %s", vtepName)
	}

	// check if the vtep has an address, if not, assign the first IP of the subnet
	existingAddrs, err := netlink.AddrList(vxlanLink, netlink.FAMILY_V4)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "get existing addresses for vxlan device %s", vtepName)
	}

	// init the IPNet for the VTEP
	hostIP, err := HostIP(subnet)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "get host IP for subnet %s", subnet)
	}

	// ATENTION: this must be used with a /32 mask
	vtepIP := &net.IPNet{
		IP:   hostIP.IP,
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	// if no address exists, assign the first IP of the subnet
	if len(existingAddrs) == 0 {
		hostIP, err := HostIP(subnet)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "get host IP for subnet %s", subnet)
		}

		if err := netlink.AddrAdd(vxlanLink, &netlink.Addr{
			IPNet: vtepIP,
		}); err != nil {
			return nil, nil, errors.Wrapf(err, "add address %s to vxlan device %s", hostIP, vtepName)
		}
	}

	// set the vxlan link up
	if err := netlink.LinkSetUp(vxlanLink); err != nil {
		return nil, nil, errors.Wrapf(err, "set vxlan device %s up", vtepName)
	}
	log.Printf("vxlan device %s created with VNI %d, MAC %s and IP %s", vtepName, vni, vtepMac, vtepIP)

	return vxlanLink, vtepIP, nil

}

func newVxlanDevice(vtepName string, vni int, vtepMac string) (*netlink.Vxlan, error) {

	mac, err := net.ParseMAC(vtepMac)
	if err != nil {
		return nil, err
	}

	gateway, err := routing.GetDefaultGatewayInterface()
	if err != nil {
		return nil, errors.Wrap(err, "getDefaultDefaultGatewayInterface")
	}

	localhosts, err := routing.GetIfaceAddr(gateway)
	if err != nil {
		return nil, errors.Wrapf(err, "get addresses for gateway interface %s", gateway.Name)
	}

	return ensureVxLan(&netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:         vtepName,
			HardwareAddr: mac,
			MTU:          gateway.MTU - config.EncapOverhead,
		},
		VxlanId:      vni,
		VtepDevIndex: gateway.Index,
		SrcAddr:      localhosts[0].IPNet.IP, // Use the first IP of the default gateway interface
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
