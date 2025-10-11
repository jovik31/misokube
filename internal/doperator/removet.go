package doperator

// removes the tenant infrastructure on the node when it receives a delete tenant event

/*func (n *NodeStoreOperator) removeTenant(key string) error {

	ctx := context.Background()

	// get the tenant
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("failed to split key %s: %w", key, err)
	}

	// fetch the tenant from cache
	tenant, err := n.TenantLister.Tenants(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("tenant %s not found in namespac %s", name, namespace)

		} else {
			return fmt.Errorf("failde to get tenant %s in nampesace %s: %w", name, namespace, err)
		}
	}

	//tenant copy
	mod := tenant.DeepCopy()

	//get the local nodestore from API
	nodestore, err := n.Base.Seterav1Clientset.SeteraV1().NodeStores(namespace).Get(ctx, n.nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get local nodestore from API %s", n.nodeName)

	}

	// check and get tenant infrastructure from the nodestore


	return nil
}*/
