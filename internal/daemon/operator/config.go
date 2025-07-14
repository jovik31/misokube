package daemon

// Config holds the configuration for the NodeStoreOperator
func (n *NodeStoreOperator) configNodestore(key string) error {

	n.Base.Logger.Info("Configuring NodeStore", "key", key)

	return nil
}
