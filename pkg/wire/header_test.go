package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"
)

// helper to craft a raw header buffer
func mkHeader(version byte, cmd Cmd, flags Flags, length uint32, magicOverride *[4]byte) []byte {
	var b [HeaderLen]byte
	if magicOverride != nil {
		b[0], b[1], b[2], b[3] = magicOverride[0], magicOverride[1], magicOverride[2], magicOverride[3]
	} else {
		b[0], b[1], b[2], b[3] = MagicA, MagicB, MagicC, MagicD
	}
	b[4] = version
	b[5] = byte(cmd)
	b[6] = byte(flags)
	// b[7] reserved
	binary.BigEndian.PutUint32(b[8:12], length)
	return b[:]
}

func TestWriteReadHeader_RoundTrip(t *testing.T) {
	want := Header{Version: 1, Cmd: HeaderCmdADD, Flags: FlagJSON, Length: 1234}

	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()

	// write from writer goroutine
	done := make(chan error, 1)
	go func() {
		done <- WriteHeader(w, want)
	}()

	// read from reader
	got, err := ReadHeader(r)
	if err != nil {
		t.Fatalf("ReadHeader error: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("WriteHeader error: %v", err)
	}

	if got != want {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", got, want)
	}
}

func TestReadHeader_BadMagic(t *testing.T) {
	// Build a buffer with wrong magic
	var bad [4]byte
	bad[0], bad[1], bad[2], bad[3] = 0x00, 0x01, 0x02, 0x03
	buf := mkHeader(1, HeaderCmdADD, FlagJSON, 10, &bad)

	h, err := ReadHeader(bytes.NewReader(buf))
	if err == nil {
		t.Fatalf("expected error, got nil; header=%+v", h)
	}
	if !errors.Is(err, ErrMagicMismatch) {
		t.Fatalf("expected ErrMagic, got: %v", err)
	}
	// Keep the context message as well
	if !stringsContain(err.Error(), "got 0 1 2 3") {
		t.Fatalf("expected context with bytes, got: %v", err)
	}
}

func TestReadHeader_PayloadTooLarge(t *testing.T) {
	// Set length to MaxPayload+1
	buf := mkHeader(1, HeaderCmdADD, FlagJSON, MaxPayload+1, nil)

	_, err := ReadHeader(bytes.NewReader(buf))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got: %v", err)
	}
	// Context shows sizes
	if !stringsContain(err.Error(), ">") {
		t.Fatalf("expected size context in error, got: %v", err)
	}
}

func TestReadHeader_ShortRead(t *testing.T) {
	// Feed fewer than HeaderLen bytes to trigger io.ReadFull error.
	short := []byte{MagicA, MagicB, MagicC} // only 3 bytes
	_, err := ReadHeader(bytes.NewReader(short))
	if err == nil {
		t.Fatal("expected io error, got nil")
	}
	// Should NOT look like our sentinel errors
	if errors.Is(err, ErrMagicMismatch) || errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("expected generic io error, got sentinel: %v", err)
	}
}

func TestWriteHeader_WritesExactBytes(t *testing.T) {
	var buf bytes.Buffer
	h := Header{Version: 7, Cmd: HeaderCmdSTATUS, Flags: FlagJSON | FlagZip, Length: 42}
	if err := WriteHeader(&buf, h); err != nil {
		t.Fatalf("WriteHeader error: %v", err)
	}
	got := buf.Bytes()
	if len(got) != HeaderLen {
		t.Fatalf("expected %d bytes, got %d", HeaderLen, len(got))
	}
	if got[0] != MagicA || got[1] != MagicB || got[2] != MagicC || got[3] != MagicD {
		t.Fatalf("bad magic in output: %v", got[:4])
	}
	if got[4] != 7 || got[5] != byte(HeaderCmdSTATUS) || got[6] != byte(FlagJSON|FlagZip) {
		t.Fatalf("bad fields: ver=%d cmd=%d flags=%d", got[4], got[5], got[6])
	}
	if binary.BigEndian.Uint32(got[8:12]) != 42 {
		t.Fatalf("bad length: %d", binary.BigEndian.Uint32(got[8:12]))
	}
}

// tiny helper (no strings import clutter elsewhere)
func stringsContain(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}
