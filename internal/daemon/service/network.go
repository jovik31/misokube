package service

import (

	// std
	"context"
	"fmt"
	"net"

	// api types
	seterav1 "github/setera/pkg/api/setera.com/v1"

	// pkg
	"github/setera/pkg/network"
)

type NetworkService struct {

	// Add fields as necessary for the network service
	NetMgr *network.NetworkManager
}

func NewNetworkService(nodeCIDR string) (*NetworkService, error) {

	if nodeCIDR == "" {

		return nil, fmt.Errorf("nodeCIDR cannot be empty")
	}

	// convert string to net.IPNet
	_, cidr, err := net.ParseCIDR(nodeCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid nodeCIDR %s: %w", nodeCIDR, err)
	}

	netMgr, err := network.NewNetworkManager(cidr)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize network manager: %w", err)
	}
	return &NetworkService{
		NetMgr: netMgr,
	}, nil
}

func (ns *NetworkService) AllocateTenant(id string) (seterav1.TenantInfra, error) {

	ctx := context.Background()
	// allocate a subnet for the tenant
	subnetRecord, err := ns.NetMgr.AllocateSubnet(ctx, id)
	if err != nil {
		return seterav1.TenantInfra{}, fmt.Errorf("failed to allocate subnet for tenant %s: %w", id, err)

	}

	tenantSubnet := subnetRecord.Network.String()

	// Create TenantInfra object
	tenantInfra := seterav1.TenantInfra{
		Name:       id,
		TenantCIDR: tenantSubnet,
		VNI:        subnetRecord.VTEP.VNI,
		VTEP_IP:    subnetRecord.VTEP.IP,
		VTEP_MAC:   subnetRecord.VTEP.MAC,
	}

	return tenantInfra, nil

}

func (ns *NetworkService) DeallocateTenant(ctx context.Context, id string) error {
	// Deallocate the tenant's subnet and clean up resources
	return ns.NetMgr.DeletetSubnet(ctx, id)
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
