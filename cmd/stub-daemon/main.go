package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github/setera/pkg/transport/uds"
	"github/setera/pkg/wire"
)

// stub-daemon: minimal UDS server to exercise the CNI shim.
// It accepts framed JSON requests and returns OK with an optional minimal result.
func main() {
	socket := os.Getenv("SOCKET")
	if socket == "" {
		socket = "/var/run/setera/setera.sock"
	}
	ln, err := uds.Listen(socket)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("stub-daemon listening on %s", socket)

	// Handle signals: cleanup socket on exit
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigc
		log.Printf("received %s, closing listener and removing socket", s)
		_ = ln.Close()
		_ = os.Remove(socket)
		os.Exit(0)
	}()

	handler := func(c net.Conn) {
		defer c.Close()
		var req wire.Request
		hdr, err := uds.ReadRequest(c, &req)
		if err != nil {
			log.Printf("read request: %v", err)
			return
		}
		log.Printf("cmd=%s cid=%s ns=%s if=%s args=%s", req.Cmd, req.ContainerID, req.NetNS, req.IfName, req.CNIArgs)

		// Build a minimal response. For ADD, return an empty current.Result scaffold.
		resp := &wire.Response{OK: true, Message: "stub ok"}
		if req.Cmd == wire.CmdADD {
			// Provide a minimal valid CNI result; shim emits scaffold if empty, but we can return one explicitly.
			ver := req.CNIVersion
			if ver == "" {
				ver = "1.1.0"
			}
			resp.Result = []byte(`{"cniVersion":"` + ver + `","interfaces":[],"ips":[],"routes":[]}`)
		}
		if err := uds.WriteResponse(c, hdr.Cmd, hdr.Flags, resp); err != nil {
			log.Printf("write response: %v", err)
		}
	}

	if err := uds.ServeLoop(ln, handler); err != nil {
		log.Printf("serve loop error: %v", err)
		_ = os.Remove(socket)
		os.Exit(1)
	}
}
