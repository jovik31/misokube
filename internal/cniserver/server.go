package cniserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github/setera/internal/podnetwork"
	"github/setera/pkg/transport/uds"
	"github/setera/pkg/wire"

	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	fallbackCNIVersion = "1.1.0"
	connectionTimeout  = 60 * time.Second
)

// podNetwork is the local Pod network API that the CNI server uses.
type podNetwork interface {
	AddPod(context.Context, podnetwork.Request) (podnetwork.Result, error)
	DelPod(context.Context, podnetwork.Request) error
	CheckPod(context.Context, podnetwork.Request) error
}

// Server receives CNI requests through a Unix domain socket.
type Server struct {
	socketPath        string
	nodeName          string
	pods              corev1listers.PodLister
	nodes             corev1listers.NodeLister
	podNetwork        podNetwork
	defaultCNIVersion string
}

// New creates a CNI server.
func New(
	socketPath string,
	nodeName string,
	pods corev1listers.PodLister,
	nodes corev1listers.NodeLister,
	podNetwork podNetwork,
	defaultCNIVersion string,
) (*Server, error) {
	if socketPath == "" {
		return nil, errors.New("cniserver: socket path is empty")
	}

	if nodeName == "" {
		return nil, errors.New("cniserver: node name is empty")
	}

	if pods == nil {
		return nil, errors.New("cniserver: pod lister is nil")
	}

	if nodes == nil {
		return nil, errors.New("cniserver: node lister is nil")
	}

	if podNetwork == nil {
		return nil, errors.New("cniserver: pod network is nil")
	}

	if defaultCNIVersion == "" {
		defaultCNIVersion = fallbackCNIVersion
	}

	return &Server{
		socketPath:        socketPath,
		nodeName:          nodeName,
		pods:              pods,
		nodes:             nodes,
		podNetwork:        podNetwork,
		defaultCNIVersion: defaultCNIVersion,
	}, nil
}

// Run serves CNI requests until the context stops or the listener fails.
func (s *Server) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	listener, err := uds.Listen(s.socketPath)
	if err != nil {
		return fmt.Errorf(
			"listen on %s: %w",
			s.socketPath,
			err,
		)
	}
	defer listener.Close()

	stop := make(chan struct{})
	defer close(stop)

	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()

		case <-stop:
		}
	}()

	log.Printf(
		"cniserver listening on %s",
		s.socketPath,
	)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			if netErr, ok := err.(net.Error); ok &&
				netErr.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}

			return fmt.Errorf(
				"accept CNI connection: %w",
				err,
			)
		}

		_ = conn.SetDeadline(
			time.Now().Add(connectionTimeout),
		)

		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(
	parent context.Context,
	conn net.Conn,
) {
	defer conn.Close()

	var req wire.Request

	header, err := uds.ReadRequest(
		conn,
		&req,
	)
	if err != nil {
		if !errors.Is(err, io.EOF) &&
			!errors.Is(err, io.ErrUnexpectedEOF) {
			log.Printf(
				"cniserver read request: %v",
				err,
			)
		}

		return
	}

	ctx, cancel := requestContext(
		parent,
		req.TimeoutSeconds,
	)
	defer cancel()

	response := s.handleRequest(ctx, &req)

	if err := uds.WriteResponse(
		conn,
		header.Cmd,
		header.Flags,
		response,
	); err != nil {
		log.Printf(
			"cniserver write response: %v",
			err,
		)
	}
}

func requestContext(
	parent context.Context,
	timeoutSeconds int,
) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		return context.WithCancel(parent)
	}

	return context.WithTimeout(
		parent,
		time.Duration(timeoutSeconds)*time.Second,
	)
}
