package main

import (
	"os"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github/setera/internal/daemon"
	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/k8s"
)

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

func main() {

	// init
	ctx := signals.SetupSignalHandler()
	logger := klog.FromContext(ctx).WithName("daemon")

	config, err := k8s.InitKubeConfig()
	if err != nil {
		logger.Error(err, "failed to fetch kubeconfig")
		os.Exit(1)
	}

	if config == nil {
		logger.Error(nil, "kubeconfig is nil, cannot proceed")
		os.Exit(1)
	}

	kubeclient, seteraclient, err := k8s.InitClients(config)
	if err != nil {
		logger.Error(err, "failed to initialize Kubernetes clients")
		os.Exit(1)
	}

	// Create nodestore
	nodename := os.Getenv("NODE_NAME")
	nodeIP := os.Getenv("NODE_IP")
	if nodename == "" || nodeIP == "" {
		logger.Error(nil, "NODE_NAME and NODE_IP environment variables must be set")
		os.Exit(1)
	}

	// create NodeStore object for the node
	_, err = seteraclient.SeteraV1().NodeStores("default").Create(ctx, &seterav1.NodeStore{

		TypeMeta: metav1.TypeMeta{
			Kind:       "NodeStore",
			APIVersion: seterav1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: nodename,
		},
		Spec: seterav1.NodeStoreSpec{
			Name:      nodename,
			NodeIP:    nodeIP,
			Selectors: nil,
		},
		Status: seterav1.NodeStoreStatus{
			Tenants: make(map[string]seterav1.TenantInfra), // Initialize with capacity for one tenant,
		},
	}, metav1.CreateOptions{})
	if err != nil {

		if apierrors.IsAlreadyExists(err) {
			logger.Info("NodeStore already exists, skipping creation", "node", nodename)
		} else {
			logger.Error(err, "failed to create NodeStore for node", "node", nodename)
			os.Exit(1)
		}
	}

	// get node_cidr
	node, err := kubeclient.CoreV1().Nodes().Get(ctx, nodename, metav1.GetOptions{})
	if err != nil {
		logger.Error(err, "failed to get node", "node", nodename)
		os.Exit(1)
	}
	cidr := node.Spec.PodCIDR
	if cidr == "" {
		logger.Error(nil, "node does not have a PodCIDR set", "node", nodename)
		os.Exit(1)
	}

	// create a new daemon instance
	daemonInstance, err := daemon.NewDaemon(ctx, "setera-daemon", nodename, nodeIP, cidr, seteraclient, kubeclient)
	if err != nil {
		logger.Error(err, " [ERROR] - failed to create daemon instance")
		os.Exit(1)
	}

	klog.Infof("[INFO][INIT] - Starting Setera Daemon on node %s: %s", nodename, nodeIP)

	// Run the NodeStore operator
	if err := daemonInstance.NodeStoreOperator.Base.Run(ctx); err != nil {
		logger.Error(err, "NodeStore operator failed to run")
		os.Exit(1)
	}

	// get kubeclientset

}
