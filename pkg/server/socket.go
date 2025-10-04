package server

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

type ConnHandler func(ctx context.Context, c net.Conn) error

// SocketServer represents a Unix domain socket server for CNI communication
type SocketServer struct {
	Path         string
	MaxConns     int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	Handler      ConnHandler

	ln     net.Listener
	wg     sync.WaitGroup
	sem    chan struct{}
	mu     sync.Mutex
	start  bool
	cancel context.CancelFunc
	logger *slog.Logger
}

type Option func(*SocketServer)

// New creates a new socket server instance
func New(path string, h ConnHandler, opts ...Option) *SocketServer {

	s := &SocketServer{
		Path:        path,
		Handler:     h,
		MaxConns:    10,
		ReadTimeout: 5 * time.Second,
	}
	return s
}

// Start starts the socket server
func (s *SocketServer) Start(ctx context.Context) error {
	// Remove existing socket file if it exists
	if err := os.RemoveAll(s.Path); err != nil {
		return err
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", s.Path)
	if err != nil {
		return err
	}
	s.ln = listener

	s.logger.Info("Socket server started", "path", s.Path)

	// Accept connections
	go s.acceptConnections(ctx)

	return nil
}

// Stop stops the socket server
func (s *SocketServer) Stop() error {
	if s.ln != nil {
		return s.ln.Close()
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
			conn, err := s.ln.Accept()
			if err != nil {
				s.logger.Error("Failed to accept connection", "error", err)
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
