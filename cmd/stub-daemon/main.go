package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github/setera/internal/daemon"
	rsv "github/setera/internal/resolver"
	"github/setera/pkg/k8s"
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

	// create and start the resolver (self-contained, non-blocking). Not injected yet.
	var res rsv.Resolver
	if restCfg, err := k8s.InitKubeConfig(); err == nil {
		r, _ := rsv.NewAndStartWithRestConfig(ctx, restCfg, rsv.Config{
			TenantLabelKey: "setera.com/tenant",
			NodeName:       os.Getenv("NODE_NAME"),
			DefaultTenant:  "default",
		})
		res = r
	}

	// create and start the CNI server routine and pass the resolver to it
	srv := daemon.NewCNIServer(socket, res)
	if err := srv.Run(); err != nil {
		log.Fatalf("cniserver run: %v", err)
	}

	// Handle signals via context; block until cancellation, then cleanup.
	<-ctx.Done()
	log.Printf("received shutdown signal, removing socket and exiting")
	_ = os.Remove(socket)
}
