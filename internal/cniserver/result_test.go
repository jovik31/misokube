package cniserver

import (
	"encoding/json"
	"net/netip"
	"testing"

	types100 "github.com/containernetworking/cni/pkg/types/100"

	"github/setera/internal/podnetwork"
	"github/setera/pkg/wire"
)

func TestEncodeAddResult(t *testing.T) {
	data, err := encodeAddResult(
		"1.1.0",
		"1.1.0",
		&wire.Request{
			IfName: "eth0",
			NetNS:  "/var/run/netns/pod-a",
		},
		podnetwork.Result{
			IP:              netip.MustParseAddr("10.244.0.10"),
			HostVethName:    "veth1234",
			HostVethIfIndex: 42,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	var result types100.Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result.CNIVersion != "1.1.0" {
		t.Fatalf("got CNI version %q, want 1.1.0", result.CNIVersion)
	}
	if len(result.Interfaces) != 2 {
		t.Fatalf("got %d interfaces, want 2", len(result.Interfaces))
	}
	if result.Interfaces[0].Name != "veth1234" {
		t.Fatalf("got host interface %q, want veth1234", result.Interfaces[0].Name)
	}
	if result.Interfaces[1].Name != "eth0" {
		t.Fatalf("got pod interface %q, want eth0", result.Interfaces[1].Name)
	}
	if result.Interfaces[1].Sandbox != "/var/run/netns/pod-a" {
		t.Fatalf("got sandbox %q", result.Interfaces[1].Sandbox)
	}
	if len(result.IPs) != 1 {
		t.Fatalf("got %d IPs, want 1", len(result.IPs))
	}
	if result.IPs[0].Address.String() != "10.244.0.10/32" {
		t.Fatalf("got address %s, want 10.244.0.10/32", result.IPs[0].Address.String())
	}
	if result.IPs[0].Interface == nil || *result.IPs[0].Interface != 1 {
		t.Fatalf("pod IP is not attached to interface index 1")
	}
}

func TestEncodeAddResultUsesDefaultVersion(t *testing.T) {
	data, err := encodeAddResult(
		"1.1.0",
		"",
		&wire.Request{
			IfName: "eth0",
			NetNS:  "/var/run/netns/pod-a",
		},
		podnetwork.Result{
			IP:           netip.MustParseAddr("10.244.0.10"),
			HostVethName: "veth1234",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	var result types100.Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.CNIVersion != "1.1.0" {
		t.Fatalf("got CNI version %q, want 1.1.0", result.CNIVersion)
	}
}

func TestEncodeAddResultRejectsInvalidIP(t *testing.T) {
	_, err := encodeAddResult(
		"1.1.0",
		"1.1.0",
		&wire.Request{IfName: "eth0"},
		podnetwork.Result{},
	)
	if err == nil {
		t.Fatal("expected invalid IP error")
	}
}
