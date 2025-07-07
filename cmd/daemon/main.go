package main

/*
Setera Daemon - Multi-tenant network orchestration agent

The daemon runs as a DaemonSet on each node and is responsible for:
- Managing network resources for tenants on the local node
- Providing a socket server for CNI plugin communication
- Watching Kubernetes resources (Tenants and NodeStores) for changes
- Coordinating with the trie-based IP allocation system

Usage:
  daemon [flags]

Flags:
  --node-name string       Name of the Kubernetes node (default: hostname)
  --node-ip string         IP address of the node
  --socket-path string     Path for the CNI socket (default: /var/run/setera/setera.sock)
  --root-cidr string       Root CIDR for IP allocation (default: 10.244.0.0/16)
  --kubeconfig string      Path to kubeconfig file
*/

/*func main() {
	var (
		nodeName = flag.String("node-name", getHostname(), "Name of the Kubernetes node")
		nodeIP   = flag.String("node-ip", "", "IP address of the node (required)")
		rootCIDR = flag.String("root-cidr", "10.244.0.0/16", "Root CIDR for IP allocation")
		// socketPath = flag.String("socket-path", "/var/run/setera/setera.sock", "Path for the CNI socket")
		// kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file")
	)

	klog.InitFlags(nil)
	flag.Parse()

	// Validate required flags
	if *nodeIP == "" {
		klog.Fatal("--node-ip is required")
	}

	// Parse node IP
	nodeIPAddr := net.ParseIP(*nodeIP)
	if nodeIPAddr == nil {
		klog.Fatalf("Invalid node IP: %s", *nodeIP)
	}

	// Parse root CIDR
	_, rootNet, err := net.ParseCIDR(*rootCIDR)
	if err != nil {
		klog.Fatalf("Invalid root CIDR %s: %v", *rootCIDR, err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		klog.Info("Received shutdown signal")
		cancel()
	}()

	// Initialize network manager with trie
	ipTrie := trie.NewTrie(rootNet)
	ipTrie.Build(30) // Build trie down to /30 subnets

	_ = &network.NetworkManager{
		Trie:        ipTrie,
		SubnetTable: make(map[string]*network.SubnetRecord),
	}

	klog.Infof("Starting Setera daemon on node %s (%s)", *nodeName, nodeIPAddr.String())
	klog.Info("Network manager initialized with trie-based IP allocation")
	klog.Info("TODO: Complete k8s client setup and daemon initialization")

	// Wait for shutdown signal
	<-ctx.Done()

	klog.Info("Setera daemon stopped")
}

// getHostname returns the system hostname or "unknown" if it cannot be determined
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}*/
