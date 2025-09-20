package service

/*
import (

	// std
	"context"
	"fmt"
	"net"

	// api types
	seterav1 "github/setera/pkg/api/setera.com/v1"

	// pkg

	"github/setera/pkg/network"
	// backend
)

type NetworkService struct {

	// Add fields as necessary for the network service
	NetMgr *network.NetworkManager
}

func NewNetworkService(nodeCIDR string, nodeName string) (*NetworkService, error) {

	if nodeCIDR == "" {

		return nil, fmt.Errorf("nodeCIDR cannot be empty")
	}

	// convert string to net.IPNet
	_, cidr, err := net.ParseCIDR(nodeCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid nodeCIDR %s: %w", nodeCIDR, err)
	}

	netMgr, err := network.NewNetworkManager(cidr, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize network manager: %w", err)
	}
	return &NetworkService{
		NetMgr: netMgr,
	}, nil
}

func (ns *NetworkService) AllocateTenant(id string) (*seterav1.TenantInfra, error) {

	// check if the tenant already exists
	if sr, exists := ns.NetMgr.SubnetRecords[id]; exists {

		return &seterav1.TenantInfra{
			Name:        id,
			TenantCIDR:  sr.Network,
			BRIDGE_NAME: sr.Bridge.Name,
			BRIDGE_IP:   sr.Bridge.IPaddress,
			BRIDGE_MAC:  sr.Bridge.MACaddress,
			VTEP_NAME:   sr.VTEP.Name,
			VNI:         sr.VTEP.VNI,
			VTEP_IP:     sr.VTEP.IP,
			VTEP_MAC:    sr.VTEP.MAC,
		}, nil
	}

	// allocate a subnet for the tenant
	subnetRecord, err := ns.NetMgr.RegisterTenant(id)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate subnet for tenant %s: %w", id, err)
	}

	// create subnet record

	ti := &seterav1.TenantInfra{
		Name:        id,
		TenantCIDR:  subnetRecord.Network,
		BRIDGE_NAME: subnetRecord.Bridge.Name,
		BRIDGE_IP:   subnetRecord.Bridge.IPaddress,
		BRIDGE_MAC:  subnetRecord.Bridge.MACaddress,
		VNI:         subnetRecord.VTEP.VNI,
		VTEP_NAME:   subnetRecord.VTEP.Name,
		VTEP_IP:     subnetRecord.VTEP.IP,
		VTEP_MAC:    subnetRecord.VTEP.MAC,
	}

	return ti, nil

}

// ATTENTION all this pre preprocessing can be implemented in the network manager
func (ns *NetworkService) ConfigureTenantRoutes(ctx context.Context,
	tenantName string,
	localTenant seterav1.NodeInfo,
	remoteTenant seterav1.NodeInfo) error {

	// get local tenant record
	localTenantRecord, err := ns.NetMgr.GetSubnetRecord(tenantName)
	if err != nil {
		return fmt.Errorf("failed to get local tenant record %s: %w", localTenant.Name, err)
	}
	lvtep := localTenantRecord.VTEP.Name

	// parse the remote tenant CIDR
	_, remoteTenantCIDR, err := net.ParseCIDR(remoteTenant.TenantCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse remote tenant CIDR %s: %w", remoteTenant.TenantCIDR, err)
	}
	// parse the remote tenant VTEP IP
	_, remoteTenantVtep, err := net.ParseCIDR(remoteTenant.VtepIP)

	// parse the remote tenant node IP
	remoteNodeIP := net.ParseIP(remoteTenant.NodeIP)
	remoteNodeCIDR := net.IPNet{
		IP:   remoteNodeIP,
		Mask: net.CIDRMask(16, 32), // Assuming /16 mask for node CIDR - remove magic number

	}
	// parse the remote vtep mac
	remoteTenantVtepMac, err := net.ParseMAC(remoteTenant.VtepMAC)
	if err != nil {
		return fmt.Errorf("failed to parse remote tenant VTEP MAC %s: %w", remoteTenant.VtepMAC, err)
	}

	// configure routing for the tenant's network
	return ns.NetMgr.ConfigureRoutes(lvtep,
		remoteTenantCIDR,
		&remoteNodeCIDR,
		remoteTenantVtep.IP,
		remoteTenantVtepMac)

}

func (ns *NetworkService) DeallocateTenant(ctx context.Context, id string) error {
	// Deallocate the tenant's subnet and clean up resources
	return ns.NetMgr.DeletetSubnet(ctx, id)
}

// returns the tenant's subnet record in the form of TenantInfra
func (ns *NetworkService) GetTenantRecord(id string) (*seterav1.TenantInfra, bool, error) {

	// check if tenant exists
	if _, exists := ns.NetMgr.SubnetRecords[id]; !exists {
		return nil, false, nil
	}

	sr, err := ns.NetMgr.GetSubnetRecord(id)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get subnet record for tenant %s: %w", id, err)
	}
	// convert to TenantInfra
	return &seterav1.TenantInfra{
		Name:        id,
		TenantCIDR:  sr.Network,
		BRIDGE_NAME: sr.Bridge.Name,
		BRIDGE_IP:   sr.Bridge.IPaddress,
		BRIDGE_MAC:  sr.Bridge.MACaddress,
		VNI:         sr.VTEP.VNI,
		VTEP_NAME:   sr.VTEP.Name,
		VTEP_IP:     sr.VTEP.IP,
		VTEP_MAC:    sr.VTEP.MAC,
	}, true, nil

}

/*func (ns *NetworkService) GetTenantInfra(id string) (seterav1.TenantInfra, error) {
	// Retrieve the tenant's infrastructure details
	ns.NetMgr
	defer ns.NetMgr.mu.RUnlock()

	record, exists := ns.NetMgr.SubnetRecords[id]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", id)
	}

	return record, nil
}

func (ns *NetworkService) ListTenants() ([]TenantInfra, error) {
	// List all tenants and their infrastructure details
	ns.NetMgr.mu.RLock()
	defer ns.NetMgr.mu.RUnlock()

	var tenants []TenantInfra
	for _, record := range ns.NetMgr.SubnetRecords {
		tenants = append(tenants, record)
	}

	return tenants, nil
}*/

/*func (ns *NetworkService) ConfigureTenantRoutes(id string, config TenantNetworkConfig) error {

}*/

/*func (ns *NetworkService) ConfigureTenantIPtables(id string, config TenantNetworkConfig) error {
}*/
