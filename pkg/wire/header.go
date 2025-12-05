package wire

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Fixed header for UDS framing (12 bytes).
// 0..3   magic  = 'S','T','R','A'  (pick any 4-byte magic)
// 4      ver    = framing version (1)
// 5      cmd    = one of Cmd* (byte form)
// 6      flags  = bitfield (0x01=JSON, 0x02=PROTO, 0x04=compressed)
// 7      rsvd   = 0
// 8..11  length = uint32 BE payload length
const (
	MagicA     = 0x53 // 'S'
	MagicB     = 0x54 // 'T'
	MagicC     = 0x52 // 'R'
	MagicD     = 0x41 // 'A'
	HeaderLen  = 12
	MaxPayload = 10 << 20 // 10 MiB guard
)

type Cmd byte

const (
	HeaderCmdADD    Cmd = 1
	HeaderCmdDEL    Cmd = 2
	HeaderCmdCHECK  Cmd = 3
	HeaderCmdSTATUS Cmd = 4
)

type Flags byte

const (
	FlagJSON Flags = 0x01
	FlagPB   Flags = 0x02
	FlagZip  Flags = 0x04 // reserved for compression
	FlagBIN  Flags = 0x08 // custom binary codec
)

type Header struct {
	Version byte
	Cmd     Cmd
	Flags   Flags
	Length  uint32
}

func WriteHeader(w io.Writer, h Header) error {
	var b [HeaderLen]byte
	b[0], b[1], b[2], b[3] = MagicA, MagicB, MagicC, MagicD
	b[4] = h.Version
	b[5] = byte(h.Cmd)
	b[6] = byte(h.Flags)
	// b[7] reserved (0)
	binary.BigEndian.PutUint32(b[8:12], h.Length)
	_, err := w.Write(b[:])
	return err
}

func ReadHeader(r io.Reader) (Header, error) {
	var b [HeaderLen]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return Header{}, err
	}
	if b[0] != MagicA || b[1] != MagicB || b[2] != MagicC || b[3] != MagicD {
		return Header{}, fmt.Errorf("%w: got %x %x %x %x", ErrMagicMismatch, b[0], b[1], b[2], b[3])
	}
	h := Header{
		Version: b[4],
		Cmd:     Cmd(b[5]),
		Flags:   Flags(b[6]),
		Length:  binary.BigEndian.Uint32(b[8:12]),
	}
	if h.Length > MaxPayload {
		return Header{}, fmt.Errorf("%w: %d > %d", ErrPayloadTooLarge, h.Length, MaxPayload)
	}
	return h, nil
}

// Helpers to map between wire.Cmd (string) and header Cmd (byte).
func CommandToHeader(cmd Command) Cmd {
	switch cmd {
	case CmdADD:
		return HeaderCmdADD
	case CmdDEL:
		return HeaderCmdDEL
	case CmdCHECK:
		return HeaderCmdCHECK
	case CmdSTATUS:
		return HeaderCmdSTATUS
	default:
		return 0
	}
}
