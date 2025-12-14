package main

import (
	"flag"
	"os"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/k8s"

	// daemon operator
	doper "github/setera/internal/daemon"
	seterainformers "github/setera/pkg/generated/informers/externalversions"
	seterav1informers "github/setera/pkg/generated/informers/externalversions/setera.com/v1"
	op "github/setera/pkg/operator"
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

type daemonConfig struct {

	// from env vars (k8s api fallback)
	nodeName string
	nodeIP   string

	// from cni conf file
	socketPath string

	//from k8s api node object
	nodeCIDR string

	// from CMD args
	backend  string // network backend: bridge, macvlan, etc.
	ipam     string // ipam strategy - default: bitmap
	arp      string // arp manager - default: netlink
	fdb      string // fdb manager - default: netlink
	route    string // route manager - default: netlink
	iptables string // iptables manager - default: iptables
	subnet   string // subnet manager - default: trie
}

func parseFlags() daemonConfig {

	var cfg daemonConfig
	flag.StringVar(&cfg.backend, "backend", "bridge-vtep", "Network backend to use, default: bridge-vtep")
	flag.StringVar(&cfg.ipam, "ipam", "bitmap", "IPAM strategy to use (default: bitmap)")
	flag.StringVar(&cfg.arp, "arp", "netlink", "ARP manager to use (default: netlink)")
	flag.StringVar(&cfg.fdb, "fdb", "netlink", "FDB manager to use (default: netlink)")
	flag.StringVar(&cfg.route, "route", "netlink", "Route manager to use (default: netlink)")
	flag.StringVar(&cfg.iptables, "iptables", "iptables", "IPTables manager to use (default: iptables)")
	flag.StringVar(&cfg.subnet, "subnet", "trie", "Subnet manager to use (default: trie)")
	// optional: root CIDR for local NM (fallback to env ROOT_CIDR)
	// flag.StringVar(&cfg.rootCIDR, "root-cidr", "10.244.0.0/16", "Root CIDR for tenant IPAM trie")

	flag.Parse()
	return cfg

}

func initNodestore(cfg *daemonConfig) *seterav1.NodeStore {

	return &seterav1.NodeStore{

		TypeMeta: metav1.TypeMeta{
			Kind:       "NodeStore",
			APIVersion: seterav1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.nodeName,
			Namespace: metav1.NamespaceNone, // NodeStores are cluster-scoped; always use NamespaceNone
		},
		Spec: seterav1.NodeStoreSpec{
			Name:      cfg.nodeName,
			NodeIP:    cfg.nodeIP,
			Selectors: nil,
		},
		Status: seterav1.NodeStoreStatus{
			Tenants: make(map[string]seterav1.TenantInfra), // Initialize with capacity for one tenant,
		},
	}

}

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

	kubeclient, seteraClient, err := k8s.InitClients(config)
	if err != nil {
		logger.Error(err, "failed to initialize Kubernetes clients")
		os.Exit(1)
	}

	// parse flags
	cfg := parseFlags()

	// NODE_NAME and NODE_IP must be set via env vars by the DaemonSet
	cfg.nodeName = os.Getenv("NODE_NAME")
	cfg.nodeIP = os.Getenv("NODE_IP")
	if cfg.nodeName == "" || cfg.nodeIP == "" {
		logger.Error(nil, "NODE_NAME and NODE_IP environment variables must be set")
		os.Exit(1)
	}

	// create nodestore object for the node

	nd := initNodestore(&cfg)
	_, err = seteraClient.SeteraV1().NodeStores(metav1.NamespaceNone).Create(ctx, nd, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("NodeStore already exists, skipping creation", "node", cfg.nodeName)
		} else {
			logger.Error(err, "failed to create NodeStore for node", "node", cfg.nodeName)
			os.Exit(1)
		}
	}

	// get node_cidr
	node, err := kubeclient.CoreV1().Nodes().Get(ctx, cfg.nodeName, metav1.GetOptions{})
	if err != nil {
		logger.Error(err, "failed to get node", "node", cfg.nodeName)
		os.Exit(1)
	}

	if node.Spec.PodCIDR == "" {
		logger.Error(nil, "node does not have a PodCIDR set", "node", cfg.nodeName)
		os.Exit(1)
	}
	cfg.nodeCIDR = node.Spec.PodCIDR

	// Start daemon operator (informers + reconciler)
	factory := seterainformers.NewSharedInformerFactory(seteraClient, 0)
	v1 := seterav1informers.New(factory, metav1.NamespaceNone, nil)
	tenantInf := v1.Tenants().Informer()
	tenantLister := v1.Tenants().Lister()
	nodeStoreInf := v1.NodeStores().Informer()
	nodeStoreLister := v1.NodeStores().Lister()

	base := op.NewBaseOperator("daemon", logger, nil)
	daemonOp := doper.New(base, logger, nil, seteraClient, kubeclient, tenantInf, tenantLister, nodeStoreInf, nodeStoreLister, nil)
	if daemonOp == nil {
		logger.Error(nil, "failed to construct daemon operator")
		os.Exit(1)
	}

	// Dispatcher is intentionally not wired here; use stub-daemon for iterative testing.
	factory.Start(ctx.Done())
	if err := daemonOp.Run(ctx); err != nil {
		logger.Error(err, "daemon operator failed")
		os.Exit(1)
	}
}
