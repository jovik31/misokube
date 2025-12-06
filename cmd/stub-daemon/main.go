package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github/setera/internal/daemon"
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

	// Start the CNI server routine (resolver disabled in stub rollback)
	srv := daemon.NewCNIServer(socket)
	if err := srv.Run(); err != nil {
		log.Fatalf("cniserver run: %v", err)
	}

	// Handle signals via context; block until cancellation, then cleanup.
	<-ctx.Done()
	log.Printf("received shutdown signal, removing socket and exiting")
	_ = os.Remove(socket)
}
