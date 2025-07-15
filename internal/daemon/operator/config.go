package daemon

// Config holds the configuration for the NodeStoreOperator
func (n *NodeStoreOperator) configNodestore(key string) error {

	n.Base.Logger.Info("Configuring NodeStore", "key", key)

	// check the tenant where this node is in the awaiting node array

	// for all the tenants check if it is already configured in this node (nodestore.Status.Tenants)
	// if so get the tenant infra and check the vailidity of the configuration and return.

	// for each tenant where it is an awaiting node 

		//network config

		// allocate tenant network[IP CIDR]
		// vni 1 identifies the default tenant
		// calculate VNI - hash with limits from 2-16777214 - same tenant name same int
		// allocate tenant vtep[IP, MAC]
		// allocate tenant bridge[IP, MAC]
		// add local routes


		// add network info to nodestore

		// route config - inter node communication
		// check if tenant has configedNodes

			// for each config node in the same tenant
				// check if routes already exist				
					// fdb
					// arp
					// routes
		

		// On this node store check if there is more tenants
			// If the tenant is not default
				// Add iptable rules to it

		// Observe the default tenant

			// Create SNAT rules for the tenant comunication
			// Create Forward rules for the node CIDR

		// Configure the tenant in the nodestore
	return nil
}
