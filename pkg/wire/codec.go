package wire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
)

type Codec interface {
	Marshal(any) ([]byte, error)
	Unmarshal([]byte, any) error
	Name() string
}

type JSONCodec struct{}

func (j JSONCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (j JSONCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (j JSONCodec) Name() string {
	return "json"
}

// BinaryCodec encodes Request/Response using a compact, stable TLV-like format.
// Field order is fixed; strings/bytes are length-prefixed with uvarint.
type BinaryCodec struct{}

func (b BinaryCodec) Name() string { return "bin" }

func writeUvar(buf *bytes.Buffer, v uint64) {
	var tmp [10]byte
	n := binary.PutUvarint(tmp[:], v)
	buf.Write(tmp[:n])
}

func writeBytes(buf *bytes.Buffer, data []byte) {
	writeUvar(buf, uint64(len(data)))
	buf.Write(data)
}

func writeString(buf *bytes.Buffer, s string) {
	writeBytes(buf, []byte(s))
}

func readUvar(b *bytes.Reader) (uint64, error) {
	// bytes.Reader implements io.ByteReader via ReadByte method
	return binary.ReadUvarint(b)
}

func readBytes(r *bytes.Reader) ([]byte, error) {
	ln, err := readUvar(r)
	if err != nil {
		return nil, err
	}
	if ln == 0 {
		return []byte{}, nil
	}
	if ln > uint64(r.Len()) {
		return nil, errors.New("binary codec: length exceeds buffer")
	}
	out := make([]byte, ln)
	if _, err := r.Read(out); err != nil {
		return nil, err
	}
	return out, nil
}

func readString(r *bytes.Reader) (string, error) {
	b, err := readBytes(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Marshal supports *Request / Request and *Response / Response.
func (b BinaryCodec) Marshal(v any) ([]byte, error) {
	switch t := v.(type) {
	case *Request:
		return b.marshalRequest(*t)
	case Request:
		return b.marshalRequest(t)
	case *Response:
		return b.marshalResponse(*t)
	case Response:
		return b.marshalResponse(t)
	default:
		return nil, errors.New("binary codec: unsupported type")
	}
}

func (b BinaryCodec) marshalRequest(r Request) ([]byte, error) {
	var buf bytes.Buffer
	// Version tag for payload layout
	buf.WriteByte(1)
	writeString(&buf, string(r.Cmd))
	writeString(&buf, r.TraceID)
	writeString(&buf, r.CNIVersion)
	writeString(&buf, r.ContainerID)
	writeString(&buf, r.NetNS)
	writeString(&buf, r.IfName)

	writeUvar(&buf, uint64(r.TimeoutSeconds))
	writeString(&buf, r.IdemKey)
	return buf.Bytes(), nil
}

func (b BinaryCodec) marshalResponse(r Response) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(1)
	// OK as 1/0
	if r.OK {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	writeString(&buf, r.Code)
	writeString(&buf, r.Message)
	writeBytes(&buf, []byte(r.Result))
	return buf.Bytes(), nil
}

// Unmarshal supports *Request and *Response destinations.
func (b BinaryCodec) Unmarshal(data []byte, out any) error {
	rd := bytes.NewReader(data)
	// payload version
	v, err := rd.ReadByte()
	if err != nil {
		return err
	}
	if v != 1 {
		return errors.New("binary codec: unknown payload version")
	}
	switch o := out.(type) {
	case *Request:
		cmd, err := readString(rd)
		if err != nil {
			return err
		}
		o.Cmd = Command(cmd)
		if o.TraceID, err = readString(rd); err != nil {
			return err
		}
		if o.CNIVersion, err = readString(rd); err != nil {
			return err
		}
		if o.ContainerID, err = readString(rd); err != nil {
			return err
		}
		if o.NetNS, err = readString(rd); err != nil {
			return err
		}
		if o.IfName, err = readString(rd); err != nil {
			return err
		}

		tsec, err := readUvar(rd)
		if err != nil {
			return err
		}
		o.TimeoutSeconds = int(tsec)
		if o.IdemKey, err = readString(rd); err != nil {
			return err
		}
		return nil
	case *Response:
		okb, err := rd.ReadByte()
		if err != nil {
			return err
		}
		o.OK = okb == 1
		if o.Code, err = readString(rd); err != nil {
			return err
		}
		if o.Message, err = readString(rd); err != nil {
			return err
		}
		raw, err := readBytes(rd)
		if err != nil {
			return err
		}
		o.Result = raw
		return nil
	default:
		return errors.New("binary codec: unsupported out type")
	}
}

// DetectFlags returns the appropriate wire.Flags for a codec.
func DetectFlags(c Codec) Flags {
	switch c.Name() {
	case "json":
		return FlagJSON
	case "bin":
		return FlagBIN
	default:
		return 0
	}
}
