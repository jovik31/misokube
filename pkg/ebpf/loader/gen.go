package loader

// Generate Go bindings from Setera BPF C programs using bpf2go.
//
// Run from the repository root:
//
//     go generate ./pkg/ebpf/loader
//
// Prerequisites:
//   - clang
//   - the Linux/BPF headers required by the programs
//
// The generated Go bindings and object files live in this package.

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -no-strip -cc clang -target amd64 -type fw_rule_key -type lpm_v4_key -type fw_rule_val -type iface_config -type pkt_stats xdpFirewall ../bpf/xdp_firewall.c -- -I../bpf -Wall -O2 -g
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -no-strip -cc clang -target amd64 -type iface_config -type pkt_stats -type veth_tenant tcFirewall ../bpf/tc_router.c -- -I../bpf -Wall -O2 -g
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -no-strip -cc clang -target amd64 -type iface_config -type pkt_stats -type veth_tenant nodeRouter ../bpf/node_router.c -- -I../bpf -Wall -O2 -g
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -no-strip -cc clang -target amd64 serviceLB ../bpf/service_lb.c -- -I../bpf -Wall -O2 -g
