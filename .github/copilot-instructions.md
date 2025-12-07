# Copilot Instructions for Setera Orchestrator

These guidelines make AI agents immediately productive in this repo. Focus on the existing design and workflows; avoid introducing new patterns unless asked.

## Big Picture
- **Goal:** Multi-node tenant networking for Kubernetes with strict isolation. Components: `orchestrator` (global), `daemon` (per-node), `CNI` (pod attach), `webhook` (validation), and CRDs `Tenant` and `NodeStore`.
- **Primary flows:**
  - Tenant placement and lifecycle via `internal/orchestrator` and `pkg/operator`.
  - Node-local networking setup via `internal/daemon/*` + `pkg/network/*` and `pkg/cni`.
  - State exchange through CRDs in `pkg/generated/*` and `pkg/k8s` helpers.
- **Networking model:** Expandable subnet (IP trie + bitmap IPAM), optional dual-IP bridge and VTEP migration to preserve active connections. See `docs/*.md` and `pkg/network/*`.

## Key Code Areas
- `cmd/*`: Binary entrypoints for `orchestrator`, `daemon`, `cni`, `webhook`.
- `internal/orchestrator/*`: Controllers and handlers for Tenants, NodeStore events, reconciliation loops.
- `internal/daemon/*`: Node-local operator wiring; reacts to Tenants and updates NodeStore.
- `pkg/network/*`: Netlink abstractions for vxlan, bridge, fdb, routes, arp, etc.
- `pkg/cni/*`: CNI command handlers and utilities for pod attach/detach.
- `pkg/k8s/*` and `pkg/generated/*`: Clientsets, listers, informers, and CRD apply configurations.
- `pkg/resolver/*`: Config resolution, snapshots, and error types used across components.
- `docs/*.md` and `README.md`: Architectural rationale and state machines for expansion and migration.

## CRDs and Status Patterns
- **Tenant**: Desired zones and (optional) `desiredPrefix`; status fields include `assignedNodes`, `allocatedPrefix`, `expansionPhase`, `vtepPhase`, and conditions. Controllers add a finalizer and drive expansion/migration. Reference: `internal/orchestrator/*`, `docs/*`.
- **NodeStore**: Per-node view of tenants including `networkCIDR`, IPAM capacity/usage, VTEP and bridge phases, `selfAck`, and a derived `peerSummary` (total/acked/pending) computed locally via indexers. Do not write to other nodes’ NodeStores.

## Conventions and Patterns
- **Deterministic IDs:**
  - VXLAN VNI from tenant hash and stable non-zero 24-bit int.
  - VTEP MAC from `tenantID|nodeName` with locally administered bit set.
  - Bridge name: `br-<hash(tenant)>`.
- **Expansion logic:** Merge sibling leaves to parent; update IPAM bitmap and `networkCIDR`. Use dual-phase migration for gateway/VTEP when needed.
- **Indexers and summaries:** Compute `peerSummary` by reading all NodeStores; patch only local NodeStore when values change.
- **Apply/patch style:** Prefer `applyconfiguration` clients or `status` patch helpers in `pkg/generated/*` and `pkg/k8s/*`. Keep patches small and idempotent.

## Developer Workflows
- **Code generation:** `make generate-code` (CRD clients, informers, listers). `make crd` and `make rbac-*` for manifests.
- **Lint/format/vet:** `make fmt`, `make vet`, `make lint` (requires `golangci-lint`).
- **Cluster setup:**
  - Kind: `make kind-cluster-dev` (orchestrator dev), `make kind-cluster-delete`.
  - Install CRDs/RBAC: `make install`.
- **Build images:** `make build-orchestrator`, `make build-daemon` (Dockerfile builds with `BINARY` arg). Load into Kind: `make kind-cluster-load-daemon-image`.
- **Run locally:** `make run-orchestrator` to run out-of-cluster orchestrator. Daemon can be applied via `config/cluster/local_daemon.yaml` using `make daemon` (generates code, CRDs, builds, applies).
- **Webhook TLS:** `make webhook-ssl` generates dev certs in `${TMPDIR}/k8s-webhook-server/serving-certs`.

## Integration Touchpoints
- **Kubernetes:** Client-go + controller-runtime; generated clients in `pkg/generated/*`, helpers in `pkg/k8s/*`.
- **CNI:** `pkg/cni/cmd_handlers.go` implements ADD/DEL flows, calling into daemon over local transport.
- **Transport:** gRPC/HTTP/UDS abstractions under `pkg/transport/*`; server helpers in `pkg/server/http.go`.
- **Wire/Headers:** Networking serialization helpers under `pkg/wire/*` (e.g., `header.go`). Follow existing types for inter-process messages.

## Testing and Benchmarks
- Resolver tests and benchmarks live in `pkg/resolver/*`. When adding logic that chooses or snapshots configs, extend these tests.
- Orchestrator tests exist in `internal/orchestrator/*` (indexers, tenant handlers). Keep handler and reconciler functions unit-testable.

## Safe Changes Checklist (examples)
- When expanding a tenant:
  - Update trie/IPAM and `NodeStore.status.networkCIDR` locally.
  - If migrating gateway/VTEP, set local phases and `selfAck`, recompute `peerSummary`.
  - Orchestrator transitions `Tenant.status.expansionPhase` and `vtepPhase` once all nodes ack.
- When attaching pods:
  - Use `pkg/cni` handlers; allocate IP via daemon’s IPAM and configure veth, routes, and bridge.

## Tips for Agents
- Prefer idempotent netlink operations (`Ensure*` helpers). Avoid recreating devices unless immutables mismatch.
- Patch-only what changed. Avoid broad updates to reduce etcd write pressure.
- Never write to remote NodeStores; use indexers to derive summaries.
- Reference `docs/*.md` before changing expansion/migration flows; keep state machines consistent.

Questions or gaps? Tell us which areas need more examples (e.g., specific applyconfiguration usage, indexer keys, or transport endpoints), and we’ll refine this doc.
