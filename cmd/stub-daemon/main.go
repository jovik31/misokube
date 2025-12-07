package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github/setera/internal/daemon"
	"net"
	"github/setera/internal/router"
	"github/setera/internal/nmanager"
	rsv "github/setera/internal/resolver"
	"github/setera/pkg/k8s"
	// applyconfiguration + meta for NodeStore ensure
	applyseterav1 "github/setera/pkg/generated/applyconfiguration/setera.com/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// stub-daemon: minimal UDS server to exercise the CNI shim.
// It accepts framed JSON requests and returns OK with an optional minimal result.
func main() {

	// Context and start
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Determine socket path from env or use default.
	socket := os.Getenv("SOCKET")
	if socket == "" {
		socket = "/var/run/setera/setera.sock"
	}

	//create and start the dispatcher (self-contained, non-blocking). Not injected yet.

	//create the router (self-contained, non-blocking). Not injected yet.

	//create the dameon operator (self-contained, non-blocking). Not injected yet.

	// create and start the resolver (self-contained, non-blocking). Not injected yet.
	var res rsv.Resolver
	var restCfg *rest.Config
	if cfg, err := k8s.InitKubeConfig(); err == nil {
		restCfg = cfg
		r, _ := rsv.NewAndStartWithRestConfig(ctx, restCfg, rsv.Config{
			TenantLabelKey: "setera.com/tenant",
			NodeName:       os.Getenv("NODE_NAME"),
			DefaultTenant:  "default",
		})
		res = r
	}

	// Ensure NodeStore CR for this node (create-or-patch via Apply)
	if restCfg != nil {
		_, seteraClient, err := k8s.InitClients(restCfg)
		if err != nil {
			log.Printf("setera client init failed: %v", err)
		} else {
			ns := os.Getenv("NAMESPACE")
			if ns == "" {
				ns = "default"
			}
			nodeName := os.Getenv("NODE_NAME")
			if nodeName == "" {
				nodeName = "unknown-node"
			}
			// Use node name as the NodeStore name for one-per-node semantics
			apply := applyseterav1.NodeStore(nodeName, ns)
			// Minimal labels to identify ownership
			apply.WithLabels(map[string]string{"setera.com/node": nodeName})
			// Apply with force to create or update
			if _, err := seteraClient.SeteraV1().NodeStores(ns).Apply(
				ctx,
				apply,
				metav1.ApplyOptions{FieldManager: "stub-daemon", Force: true},
			); err != nil {
				log.Printf("failed to apply NodeStore %s/%s: %v", ns, nodeName, err)
			} else {
				log.Printf("ensured NodeStore %s/%s", ns, nodeName)
			}
		}
	}

	// create and start the CNI server routine and pass the resolver to it
	srv := daemon.NewCNIServer(socket, res)
	// Initialize NetworkManager for routing to tenant actors
	nodeName := os.Getenv("NODE_NAME")
	root := os.Getenv("ROOT_CIDR")
	if root == "" {
		root = "10.0.0.0/16"
	}
	if _, rootCIDR, err := net.ParseCIDR(root); err == nil {
		if nm, err := nmanager.NewNetworkManager(rootCIDR, nodeName); err == nil && nm != nil {
			// Wire router to NetworkManager's tenant actors lookup
			lookup := func(tenantID string) (router.TenantActor, bool) { return nm.GetTenantActor(tenantID) }
			srv.SetRouter(router.NewNManagerRouter(lookup))
		} else if err != nil {
			log.Printf("network manager init failed: %v", err)
		}
	} else {
		log.Printf("invalid ROOT_CIDR %q: %v", root, err)
	}
	// Replace this with a real nmanager-backed lookup once tenant actors are implemented.
	// Router lookup is wired once NetworkManager is initialized; placeholder remains for now.
	// lookup := func(tenantID string) (router.TenantActor, bool) { return nil, false }
	// srv.SetRouter(router.NewNManagerRouter(lookup))
	if err := srv.Run(); err != nil {
		log.Fatalf("cniserver run: %v", err)
	}

	// Handle signals via context; block until cancellation, then cleanup.
	<-ctx.Done()
	log.Printf("received shutdown signal, removing socket and exiting")
	_ = os.Remove(socket)
}
