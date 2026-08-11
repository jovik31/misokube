// Package ebpf provides low-level eBPF datapath primitives used by Setera.
//
// This package owns mechanisms such as:
//   - loading Setera eBPF programs;
//   - attaching and detaching programs from Linux interfaces;
//   - reading and updating Setera BPF maps.
//
// It deliberately does not own Kubernetes or Setera lifecycle state.
// In particular, this package must not:
//   - watch Pods, Nodes, or Tenants;
//   - track Pod lifecycle by Pod UID or name;
//   - decide tenant placement or policy;
//   - perform retries or reconciliation.
//
// Higher-level lifecycle and reconciliation belong in internal/ebpfmanager.
// Linux networking operations that do not require eBPF belong in pkg/network.
package ebpf
