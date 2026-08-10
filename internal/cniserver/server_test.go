package cniserver

import (
	"context"
	"errors"
	"testing"
)

func TestNewValidatesDependencies(t *testing.T) {
	pods := newPodLister(t)
	network := &fakePodNetwork{}

	tests := []struct {
		name       string
		socketPath string
		podsNil    bool
		networkNil bool
	}{
		{
			name:       "missing socket",
			socketPath: "",
		},
		{
			name:       "missing pod lister",
			socketPath: "/tmp/setera.sock",
			podsNil:    true,
		},
		{
			name:       "missing pod network",
			socketPath: "/tmp/setera.sock",
			networkNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPods := pods
			if tt.podsNil {
				gotPods = nil
			}

			var gotNetwork podNetwork = network
			if tt.networkNil {
				gotNetwork = nil
			}

			if _, err := New(tt.socketPath, gotPods, gotNetwork, "1.1.0"); err == nil {
				t.Fatal("expected constructor error")
			}
		})
	}
}

func TestNewUsesFallbackCNIVersion(t *testing.T) {
	server, err := New(
		"/tmp/setera.sock",
		newPodLister(t),
		&fakePodNetwork{},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	if server.defaultCNIVersion != fallbackCNIVersion {
		t.Fatalf(
			"got default CNI version %q, want %q",
			server.defaultCNIVersion,
			fallbackCNIVersion,
		)
	}
}

func TestRunRejectsCancelledContext(t *testing.T) {
	server, err := New(
		"/tmp/setera.sock",
		newPodLister(t),
		&fakePodNetwork{},
		"1.1.0",
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := server.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}
