package ebpf

import (
	"errors"
	"testing"
)

func TestAttachNodeProgram(t *testing.T) {
	handle := &fakeNodeProgramHandle{}

	var gotIfName string

	program, err := attachNodeProgram(
		"setera0",
		func(ifName string) (nodeProgramHandle, error) {
			gotIfName = ifName
			return handle, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if program == nil {
		t.Fatal("expected program")
	}
	if gotIfName != "setera0" {
		t.Fatalf(
			"ifName = %q, want %q",
			gotIfName,
			"setera0",
		)
	}
}

func TestAttachNodeProgramRejectsEmptyInterface(t *testing.T) {
	tests := []string{
		"",
		"   ",
	}

	for _, ifName := range tests {
		t.Run(ifName, func(t *testing.T) {
			called := false

			_, err := attachNodeProgram(
				ifName,
				func(string) (nodeProgramHandle, error) {
					called = true
					return &fakeNodeProgramHandle{}, nil
				},
			)
			if err == nil {
				t.Fatal("expected error")
			}
			if called {
				t.Fatal("attacher called for invalid input")
			}
		})
	}
}

func TestAttachNodeProgramRejectsNilAttacher(t *testing.T) {
	_, err := attachNodeProgram("setera0", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAttachNodeProgramWrapsAttachError(t *testing.T) {
	wantErr := errors.New("attach failed")

	_, err := attachNodeProgram(
		"setera0",
		func(string) (nodeProgramHandle, error) {
			return nil, wantErr
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf(
			"error = %v, want wrapped %v",
			err,
			wantErr,
		)
	}
}

func TestAttachNodeProgramRejectsNilHandle(t *testing.T) {
	_, err := attachNodeProgram(
		"setera0",
		func(string) (nodeProgramHandle, error) {
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNodeProgramCloseIsIdempotent(t *testing.T) {
	handle := &fakeNodeProgramHandle{}

	program, err := attachNodeProgram(
		"setera0",
		func(string) (nodeProgramHandle, error) {
			return handle, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := program.Close(); err != nil {
		t.Fatal(err)
	}
	if err := program.Close(); err != nil {
		t.Fatal(err)
	}

	if handle.closeCalls != 1 {
		t.Fatalf(
			"Close called %d times, want 1",
			handle.closeCalls,
		)
	}
}

func TestNodeProgramCloseReturnsCloseError(t *testing.T) {
	wantErr := errors.New("close failed")
	handle := &fakeNodeProgramHandle{
		closeErr: wantErr,
	}

	program, err := attachNodeProgram(
		"setera0",
		func(string) (nodeProgramHandle, error) {
			return handle, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = program.Close()
	if !errors.Is(err, wantErr) {
		t.Fatalf(
			"error = %v, want %v",
			err,
			wantErr,
		)
	}

	err = program.Close()
	if !errors.Is(err, wantErr) {
		t.Fatalf(
			"second close error = %v, want %v",
			err,
			wantErr,
		)
	}
	if handle.closeCalls != 1 {
		t.Fatalf(
			"Close called %d times, want 1",
			handle.closeCalls,
		)
	}
}

func TestNilNodeProgramClose(t *testing.T) {
	var program *NodeProgram

	if err := program.Close(); err != nil {
		t.Fatal(err)
	}
}

type fakeNodeProgramHandle struct {
	closeCalls int
	closeErr   error
}

func (f *fakeNodeProgramHandle) Close() error {
	f.closeCalls++
	return f.closeErr
}
