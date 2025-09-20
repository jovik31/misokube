package uds

import (
	"fmt"
	"io"
	"net"
	"time"

	"github/setera/pkg/wire"
)

// Client performs a single request/response over a Unix domain socket.
type Client struct {
	Codec wire.Codec // JSON today; Protobuf later
}

func NewClientJSON() *Client { return &Client{Codec: wire.JSONCodec{}} }

func (c *Client) Call(sock string, deadline time.Duration, req *wire.Request, out *wire.Response) error {
	// Dial
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.Dial("unix", sock)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(deadline))

	// Marshal body
	body, err := c.Codec.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Write header + body
	hdr := wire.Header{
		Version: 1,
		Cmd:     wire.CommandToHeader(req.Cmd),
		Flags:   wire.FlagJSON, // today
		Length:  uint32(len(body)),
	}
	if err := wire.WriteHeader(conn, hdr); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("write body: %w", err)
	}

	// Read reply header + payload
	rh, err := wire.ReadHeader(conn)
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	buf := make([]byte, rh.Length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	// Decode reply
	if err := c.Codec.Unmarshal(buf, out); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}
