package loader

import (
	"testing"

	"github.com/cilium/ebpf"
)

func TestPrepareSocketLBSpec(t *testing.T) {
	spec := &ebpf.CollectionSpec{
		Maps: map[string]*ebpf.MapSpec{
			"svc_frontend":    serviceFrontendMapSpec(),
			"svc_backend":     serviceBackendMapSpec(),
			"svc_sock_revnat": serviceSocketRevNatMapSpec(),
			"svc_sock_stats":  serviceSocketStatsMapSpec(),
		},
	}

	if err := prepareSocketLBSpec(spec); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"svc_frontend",
		"svc_backend",
		"svc_sock_revnat",
		"svc_sock_stats",
	} {
		if got := spec.Maps[name].Pinning; got != ebpf.PinNone {
			t.Fatalf("map %s pinning = %v, want PinNone", name, got)
		}
	}
}

func TestPrepareSocketLBSpecRejectsIncompatibleMap(t *testing.T) {
	frontend := serviceFrontendMapSpec()
	frontend.ValueSize++

	spec := &ebpf.CollectionSpec{
		Maps: map[string]*ebpf.MapSpec{
			"svc_frontend":    frontend,
			"svc_backend":     serviceBackendMapSpec(),
			"svc_sock_revnat": serviceSocketRevNatMapSpec(),
			"svc_sock_stats":  serviceSocketStatsMapSpec(),
		},
	}

	if err := prepareSocketLBSpec(spec); err == nil {
		t.Fatal("incompatible Service socket LB map was accepted")
	}
}
