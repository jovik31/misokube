package uds

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github/setera/pkg/wire"
)

// Listen binds a unix socket path (creating parent dir) with hardened perms.
func Listen(socketPath string) (net.Listener, error) {
	_ = os.Remove(socketPath)
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	// Ensure the socket file has restricted permissions. net.Listen("unix", ...) creates
	// the filesystem node immediately, so chmod the actual socket path (not fd 0).
	_ = os.Chmod(socketPath, 0o600)
	return ln, nil
}

// ReadRequest reads a single framed Request (JSON for now) from conn.
func ReadRequest(conn net.Conn, out *wire.Request) (wire.Header, error) {
	h, err := wire.ReadHeader(conn)
	if err != nil {
		return h, err
	}
	body := make([]byte, h.Length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return h, err
	}
	switch {
	case h.Flags&wire.FlagJSON != 0:
		if err := (wire.JSONCodec{}).Unmarshal(body, out); err != nil {
			return h, err
		}
	case h.Flags&wire.FlagBIN != 0:
		if err := (wire.BinaryCodec{}).Unmarshal(body, out); err != nil {
			return h, err
		}
	default:
		return h, fmt.Errorf("unsupported codec flags=%#x", h.Flags)
	}
	return h, nil
}

// WriteResponse writes a framed Response back to conn using the same codec indicated by flags.
func WriteResponse(conn net.Conn, cmd wire.Cmd, flags wire.Flags, resp *wire.Response) error {
	var (
		body []byte
		err  error
	)
	switch {
	case flags&wire.FlagJSON != 0:
		body, err = (wire.JSONCodec{}).Marshal(resp)
	case flags&wire.FlagBIN != 0:
		body, err = (wire.BinaryCodec{}).Marshal(resp)
	default:
		return fmt.Errorf("unsupported codec flags=%#x", flags)
	}
	if err != nil {
		return err
	}
	h := wire.Header{Version: 1, Cmd: cmd, Flags: flags, Length: uint32(len(body))}
	if err := wire.WriteHeader(conn, h); err != nil {
		return err
	}
	_, err = conn.Write(body)
	return err
}

// ServeLoop is a tiny accept loop. Hand each conn to handler in its own goroutine.
func ServeLoop(ln net.Listener, handler func(net.Conn)) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			// transient errors: keep going
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		_ = c.SetDeadline(time.Now().Add(60 * time.Second))
		go handler(c)
	}
}
