package cniserver

import (
	"context"
	"errors"
	"testing"
)

func TestNewValidatesDependencies(t *testing.T) {
	pods := newPodLister(t)
	nodes := newNodeLister(
		t,
		nodeWithTenants(t, "node-a", "tenant-a"),
	)
	network := &fakePodNetwork{}

	tests := []struct {
		name       string
		socketPath string
		nodeName   string
		podsNil    bool
		nodesNil   bool
		networkNil bool
	}{
		{
			name:       "missing socket",
			socketPath: "",
			nodeName:   "node-a",
		},
		{
			name:       "missing node name",
			socketPath: "/tmp/setera.sock",
		},
		{
			name:       "missing pod lister",
			socketPath: "/tmp/setera.sock",
			nodeName:   "node-a",
			podsNil:    true,
		},
		{
			name:       "missing node lister",
			socketPath: "/tmp/setera.sock",
			nodeName:   "node-a",
			nodesNil:   true,
		},
		{
			name:       "missing pod network",
			socketPath: "/tmp/setera.sock",
			nodeName:   "node-a",
			networkNil: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotPods := pods
			if test.podsNil {
				gotPods = nil
			}

			gotNodes := nodes
			if test.nodesNil {
				gotNodes = nil
			}

			var gotNetwork podNetwork = network
			if test.networkNil {
				gotNetwork = nil
			}

			if _, err := New(
				test.socketPath,
				test.nodeName,
				gotPods,
				gotNodes,
				gotNetwork,
				"1.1.0",
			); err == nil {
				t.Fatal("expected constructor error")
			}
		})
	}
}

func TestNewUsesFallbackCNIVersion(t *testing.T) {
	server, err := New(
		"/tmp/setera.sock",
		"node-a",
		newPodLister(t),
		newNodeLister(
			t,
			nodeWithTenants(t, "node-a", "tenant-a"),
		),
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
		"node-a",
		newPodLister(t),
		newNodeLister(
			t,
			nodeWithTenants(t, "node-a", "tenant-a"),
		),
		&fakePodNetwork{},
		"1.1.0",
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	if err := server.Run(ctx); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf(
			"got %v, want context.Canceled",
			err,
		)
	}
}
