package ebpf

import (
	"errors"
	"strings"
	"testing"
)

func TestAttachPodProgram(t *testing.T) {
	handle := &fakePodProgramHandle{}

	var gotIfName string
	var gotTenant string

	program, err := attachPodProgram(
		"veth1234",
		"tenant-a",
		func(ifName, tenant string) (podProgramHandle, error) {
			gotIfName = ifName
			gotTenant = tenant
			return handle, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if program == nil {
		t.Fatal("expected program")
	}
	if gotIfName != "veth1234" {
		t.Fatalf("ifName = %q, want %q", gotIfName, "veth1234")
	}
	if gotTenant != "tenant-a" {
		t.Fatalf("tenant = %q, want %q", gotTenant, "tenant-a")
	}
}

func TestAttachPodProgramRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		ifName string
		tenant string
	}{
		{
			name:   "empty interface",
			ifName: "",
			tenant: "tenant-a",
		},
		{
			name:   "whitespace interface",
			ifName: "   ",
			tenant: "tenant-a",
		},
		{
			name:   "empty tenant",
			ifName: "veth1234",
			tenant: "",
		},
		{
			name:   "whitespace tenant",
			ifName: "veth1234",
			tenant: "   ",
		},
		{
			name:   "tenant exceeds BPF capacity",
			ifName: "veth1234",
			tenant: strings.Repeat("a", maxTenantIDBytes+1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false

			_, err := attachPodProgram(
				tt.ifName,
				tt.tenant,
				func(_, _ string) (podProgramHandle, error) {
					called = true
					return &fakePodProgramHandle{}, nil
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

func TestAttachPodProgramWrapsAttachError(t *testing.T) {
	wantErr := errors.New("attach failed")

	_, err := attachPodProgram(
		"veth1234",
		"tenant-a",
		func(_, _ string) (podProgramHandle, error) {
			return nil, wantErr
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}

func TestAttachPodProgramRejectsNilHandle(t *testing.T) {
	_, err := attachPodProgram(
		"veth1234",
		"tenant-a",
		func(_, _ string) (podProgramHandle, error) {
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPodProgramCloseIsIdempotent(t *testing.T) {
	handle := &fakePodProgramHandle{}

	program, err := attachPodProgram(
		"veth1234",
		"tenant-a",
		func(_, _ string) (podProgramHandle, error) {
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
		t.Fatalf("Close called %d times, want 1", handle.closeCalls)
	}
}

func TestPodProgramCloseReturnsCloseError(t *testing.T) {
	wantErr := errors.New("close failed")
	handle := &fakePodProgramHandle{
		closeErr: wantErr,
	}

	program, err := attachPodProgram(
		"veth1234",
		"tenant-a",
		func(_, _ string) (podProgramHandle, error) {
			return handle, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = program.Close()
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}

	// Close must not retry a failed detach implicitly.
	err = program.Close()
	if !errors.Is(err, wantErr) {
		t.Fatalf("second close error = %v, want %v", err, wantErr)
	}
	if handle.closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", handle.closeCalls)
	}
}

func TestNilPodProgramClose(t *testing.T) {
	var program *PodProgram
	if err := program.Close(); err != nil {
		t.Fatal(err)
	}
}

type fakePodProgramHandle struct {
	closeCalls int
	closeErr   error
}

func (f *fakePodProgramHandle) Close() error {
	f.closeCalls++
	return f.closeErr
}
