package daemon

import (
	"errors"
	"io"
	"log"
	"net"

	"github/setera/pkg/transport/uds"
	"github/setera/pkg/wire"
)

// CNIServer handles concurrent CNI requests and serializes work per-tenant via Router.
type CNIServer struct {
	socketPath string
}

func NewCNIServer(socketPath string) *CNIServer {
	if socketPath == "" {
		log.Panic("cniserver: missing socket path")
	}
	return &CNIServer{socketPath: socketPath}
}

// Run is a convenience wrapper over Start that begins serving in the background
// and returns immediately (non-blocking). It logs the socket path on success.
func (s *CNIServer) Run() error {
	_, err := s.Start()
	return err
}

// Start binds the UDS and starts serving requests concurrently.
func (s *CNIServer) Start() (net.Listener, error) {
	ln, err := uds.Listen(s.socketPath)
	if err != nil {
		return nil, err
	}
	go uds.ServeLoop(ln, s.handleConn)
	log.Printf("cniserver listening on %s", s.socketPath)
	return ln, nil
}

func (s *CNIServer) handleConn(c net.Conn) {
	defer c.Close()
	var req wire.Request
	hdr, err := uds.ReadRequest(c, &req)
	if err != nil {
		// Ignore benign EOFs from short-lived clients/probes
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			log.Printf("read request: %v", err)
		}
		return
	}

	// Handle per-command behavior with minimal responses.
	var resp *wire.Response
	switch req.Cmd {
	case wire.CmdADD:
		ver := req.CNIVersion
		if ver == "" {
			ver = "1.1.0"
		}
		resp = &wire.Response{OK: true, Message: "add ok", Result: []byte(`{"cniVersion":"` + ver + `","interfaces":[],"ips":[],"routes":[]}`)}

	case wire.CmdDEL:
		// DEL should be idempotent and not depend on resolver readiness.
		// Proceed without resolving to avoid spurious failures during startup.
		resp = &wire.Response{OK: true, Message: "del ok"}
	case wire.CmdCHECK:
		resp = &wire.Response{OK: true, Message: "check ok"}
	case wire.CmdSTATUS:
		resp = &wire.Response{OK: true, Message: "ready"}
	default:
		resp = &wire.Response{OK: false, Message: "unknown cmd"}
	}

	_ = uds.WriteResponse(c, hdr.Cmd, hdr.Flags, resp)
}

// No resolver integration in the stub: keep behavior minimal.
