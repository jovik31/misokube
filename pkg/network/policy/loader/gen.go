package firewall

// Generate Go bindings from BPF C programs using bpf2go.
// Run `go generate ./...` from the eBPF_test directory to regenerate.
//
// Prerequisites:
//   - clang (>= 11)
//   - libbpf headers (libbpf-dev on Debian/Ubuntu)
//
// The -target flag builds for the native architecture only.
// Add more -target flags (e.g., arm64) for cross-compilation.

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -no-strip -cc clang -target amd64 -type fw_rule_key -type lpm_v4_key -type fw_rule_val -type iface_config -type pkt_stats xdpFirewall ../../policy/bpf/xdp_firewall.c -- -I../../policy/bpf -Wall -O2 -g
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -no-strip -cc clang -target amd64 -type fw_rule_key -type lpm_v4_key -type fw_rule_val -type iface_config -type pkt_stats -type conntrack_key -type conntrack_val tcFirewall ../../policy/bpf/tc_firewall.c -- -I../../policy/bpf -Wall -O2 -g
