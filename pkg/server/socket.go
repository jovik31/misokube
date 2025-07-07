package server

import (
	"context"
	"net"
	"os"

	"k8s.io/klog/v2"
)

// SocketServer represents a Unix domain socket server for CNI communication
type SocketServer struct {
	socketPath string
	listener   net.Listener
	logger     klog.Logger
}

// NewSocketServer creates a new socket server instance
func NewSocketServer(socketPath string, logger klog.Logger) *SocketServer {
	return &SocketServer{
		socketPath: socketPath,
		logger:     logger,
	}
}

// Start starts the socket server
func (s *SocketServer) Start(ctx context.Context) error {
	// Remove existing socket file if it exists
	if err := os.RemoveAll(s.socketPath); err != nil {
		return err
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	s.listener = listener

	s.logger.Info("Socket server started", "path", s.socketPath)

	// Accept connections
	go s.acceptConnections(ctx)

	return nil
}

// Stop stops the socket server
func (s *SocketServer) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// acceptConnections handles incoming connections
func (s *SocketServer) acceptConnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				s.logger.Error(err, "Failed to accept connection")
				continue
			}

			// Handle connection in a separate goroutine
			go s.handleConnection(conn)
		}
	}
}

// handleConnection handles a single connection
func (s *SocketServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	s.logger.Info("New CNI connection established")

	// TODO: Implement CNI request/response handling
	// This would handle:
	// - Pod network setup requests
	// - IP allocation requests
	// - Network cleanup requests

	s.logger.Info("CNI connection closed")
}
