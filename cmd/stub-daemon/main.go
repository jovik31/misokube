package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github/setera/internal/daemon"
	"github/setera/internal/dispatcher"
	"github/setera/internal/nmanager"
	rsv "github/setera/internal/resolver"
	"github/setera/internal/router"
	seterainformers "github/setera/pkg/generated/informers/externalversions"
	seterav1informers "github/setera/pkg/generated/informers/externalversions/setera.com/v1"
	"github/setera/pkg/k8s"
	op "github/setera/pkg/operator"

	seterav1 "github/setera/pkg/api/setera.com/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// stub-daemon: minimal UDS server to exercise the CNI shim.
// It accepts framed JSON requests and returns OK with an optional minimal result.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// init config
	restCfg, err := initKube()
	if err != nil {
		log.Printf("kube init failed: %v", err)
		os.Exit(1)
	}

	// init clients
	kubeclient, seteraClient, err := k8s.InitClients(restCfg)
	if err != nil {
		log.Printf("InitClients failed: %v", err)
		os.Exit(1)
	}

	// get node name and ip
	nodeName, err := k8s.GetNodeName(kubeclient)
	if err != nil {
		log.Printf("failed to get node name: %v", err)
		os.Exit(1)
	}
	nodeIP, err := k8s.GetNodeIP(kubeclient, nodeName)
	if err != nil {
		log.Printf("failed to get node IP: %v", err)
		os.Exit(1)
	}

	// get socket path
	socket := getSocketPath()

	// init resolver
	res, err := initResolver(ctx, kubeclient, nodeName)
	if err != nil {
		log.Printf("resolver init failed: %v", err)
		os.Exit(1)
	}

	// ensure NodeStore exists
	if err := ensureNodeStore(ctx, restCfg, nodeName, nodeIP); err != nil {
		log.Printf("ensure NodeStore failed: %v", err)
		os.Exit(1)
	}

	// initialize network manager
	nodeCIDR, err := k8s.GetNodeCIDR(kubeclient, nodeName)
	if err != nil {
		log.Printf("failed to get node CIDR: %v", err)
		os.Exit(1)
	}
	_, nodeCIDRParsed, err := net.ParseCIDR(nodeCIDR)
	if err != nil {
		log.Printf("invalid node CIDR %q: %v", nodeCIDR, err)
		os.Exit(1)
	}
	nm, err := nmanager.NewNetworkManager(nodeCIDRParsed, nodeName)
	if err != nil {
		log.Printf("network manager init failed: %v", err)
		os.Exit(1)
	}

	// Prepare operator base and wire NM -> operator emitter
	base := op.NewBaseOperator("stub-daemon", klog.FromContext(ctx), nil)
	nm.SetEmitter(base)

	// init dispatcher
	dp := dispatcher.New(nm)
	go func() {
		if err := dp.Run(ctx); err != nil {
			log.Printf("dispatcher stopped: %v", err)
		}
	}()

	// Initialize and run the daemon operator similarly to daemon main
	factory := seterainformers.NewSharedInformerFactory(seteraClient, 0)
	v1 := seterav1informers.New(factory, "default", nil)
	tenantInf := v1.Tenants().Informer()
	tenantLister := v1.Tenants().Lister()
	nodeStoreInf := v1.NodeStores().Informer()
	nodeStoreLister := v1.NodeStores().Lister()

	// base already initialized above
	// Inject dispatcher into operator if started; otherwise nil uses noop
	dispatcherAdapter := daemon.NewDispatcherAdapter(dp)
	dOpr := daemon.New(base, klog.FromContext(ctx), nil, seteraClient, tenantInf, tenantLister, nodeStoreInf, nodeStoreLister, dispatcherAdapter)

	// Provide local node context and NM ops to operator
	dOpr.SetNodeName(nodeName)
	dOpr.SetNMOps(nm)
	if dOpr != nil {
		factory.Start(ctx.Done())
		go func() {
			if err := dOpr.Run(ctx); err != nil {
				log.Printf("stub daemon operator stopped: %v", err)
			}
		}()
	}

	// Start CNI server
	srv := daemon.NewCNIServer(socket, res)
	if r := initRouter(nodeName); r != nil {
		srv.SetRouter(r)
	}

	if err := srv.Run(); err != nil {
		log.Fatalf("cniserver run: %v", err)
	}

	<-ctx.Done()
	log.Printf("received shutdown signal, removing socket and exiting")
	_ = os.Remove(socket)
}

// --- helpers ---

func getSocketPath() string {
	if s := os.Getenv("SOCKET"); s != "" {
		return s
	}
	return "/var/run/setera/setera.sock"
}

func initKube() (*rest.Config, error) {
	cfg, err := k8s.InitKubeConfig()
	if err != nil {
		log.Printf("InitKubeConfig failed: %v", err)
		return nil, err
	}
	return cfg, nil
}

func initResolver(ctx context.Context, kubeclient kubernetes.Interface, nodeName string) (rsv.Resolver, error) {
	if kubeclient == nil {
		return nil, fmt.Errorf("nil kubeclient")
	}

	if nodeName == "" {
		return nil, fmt.Errorf("empty node name")
	}
	r, err := rsv.NewAndStartWithClient(ctx, kubeclient, rsv.Config{
		TenantLabelKey: "setera.com/tenant",
		NodeName:       nodeName,
		DefaultTenant:  "default",
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func ensureNodeStore(ctx context.Context, cfg *rest.Config, nodeName, nodeIP string) error {
	if cfg == nil {
		return nil
	}
	_, seteraClient, err := k8s.InitClients(cfg)
	if err != nil {
		return err
	}
	if nodeName == "" {
		nodeName = "unknown-node"
	}
	nd := &seterav1.NodeStore{
		TypeMeta:   metav1.TypeMeta{Kind: "NodeStore", APIVersion: seterav1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		Spec:       seterav1.NodeStoreSpec{Name: nodeName, NodeIP: nodeIP},
		Status:     seterav1.NodeStoreStatus{Tenants: make(map[string]seterav1.TenantInfra)},
	}
	if _, err := seteraClient.SeteraV1().NodeStores("default").Create(ctx, nd, metav1.CreateOptions{}); err != nil { // always created in the default namespace
		if apierrors.IsAlreadyExists(err) {
			log.Printf("NodeStore %s already exists", nodeName)
			return nil
		}
		return err
	}
	log.Printf("created NodeStore %s", nodeName)
	return nil
}

func initRouter(nodeName string) router.Router {
	root := os.Getenv("ROOT_CIDR")
	if root == "" {
		root = "10.0.0.0/16"
	}
	if _, rootCIDR, err := net.ParseCIDR(root); err == nil {
		if nm, err := nmanager.NewNetworkManager(rootCIDR, nodeName); err == nil && nm != nil {
			lookup := func(tenantID string) (router.TenantActor, bool) { return nm.GetTenantActor(tenantID) }
			return router.NewNManagerRouter(lookup)
		}
		log.Printf("network manager init failed: %v", err)
		return nil
	}
	log.Printf("invalid ROOT_CIDR %q", root)
	return nil
}
