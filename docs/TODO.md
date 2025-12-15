# Setera Orchestrator — Working TODOs

This document tracks pending work items and simplifications discussed across daemon, orchestrator, and networking layers. It complements existing design docs and will be iterated as features land.

## Orchestrator
- Indexers correctness: attach tenant indexers only to `Tenant` informer; attach NodeStore indexers only to `NodeStore` informer. Use type-safe `IndexFunc`s and avoid cross-informer reuse. Implement:
  - Tenant indexer key (e.g., `setera.com/tenant`) on `Tenant` informer.
  - NodeStore indexers (e.g., `setera.com/nodename`, optional `setera.com/tenant`) on `NodeStore` informer.
- Verify handler ordering: add indexers before handlers and informer start.

## Daemon — NodeStore Event Handlers (from Kubernetes)
- Add handler:
  - Enqueue only for the local NodeStore (`Name == nodeName`). Purpose: seed local state or trigger local NM mirror if needed.
- Update handler (remote NodeStores only):
  - Enqueue for remote NodeStores (`Name != nodeName`).
  - Diff `status.tenants` excluding `Pods` field; enqueue only when fields affecting peering/topology change:
    - `TenantCIDR`, `VNI`, `VTEP_NAME`, `VTEP_IP`, `VTEP_MAC`, `BRIDGE_NAME`, `BRIDGE_IP`, `BRIDGE_MAC`.
  - For changed tenants, call `nm.EnsurePeer(tenantID, RemoteTenantInfra)`.
- Delete handler:
  - Local delete: remove all local tenant state (flush NM), clear peering.
  - Remote delete: enqueue `nm.RemovePeer(tenantID, remote)` for each tenant previously present on that node.
- Edge cases: empty tenants, NotFound handling on status updates, idempotency of Ensure/Remove.

## Daemon — NM → NodeStore Mirroring
- Mirror NM snapshots to local NodeStore on NM events (update/delete) via a single `mirrorLocalNodeStore()` helper.
- Always initialize `Pods` with an empty slice to satisfy CRD required constraints.
- Treat NotFound on `UpdateStatus` as non-fatal; rely on add handler or subsequent events.

## Networking Manager (NM)
- Implement and wire peering ops in dispatcher/operator path:
  - `EnsurePeer(tenantID, RemoteTenantInfra)` and `RemovePeer(tenantID, RemoteTenantInfra)`.
- Snapshot completeness:
  - Optionally include bridge IP/MAC in local snapshots.
- Idempotent netlink operations using `Ensure*` helpers.

### PodOps — Optional Tweaks
- Error surfacing: Map `ErrTenantClosing` to a retryable CNI error in the router (clear message, suggest backoff).
- Tenant states: Add `TenantStateUpdating` to gate or prioritize `UpdatePod` during expansion/migration.
- Backpressure: Expose per-tenant mailbox depth; return fast-fail when saturated; add metrics.
- Timeouts/backoff: Router-level configurable timeouts with exponential backoff/retry on transient failures.
- Observability: Emit tracing spans and metrics for `EnsurePod`/`RemovePod`/`UpdatePod` latency and error codes.
- Router API: Add explicit `UpdatePod` method and DEL path alignment for CNI, mirroring actor methods.
- Prioritization: Optionally prioritize `RemovePod` over `EnsurePod` when closing to speed teardown.

## Stub-Daemon Wiring
- Ensure Tenant informer is cluster-wide if Tenants may live outside `default`.
- Keep NodeStore scoped to `default` namespace, consistent with creation.
- Avoid duplicate NM instances between router/dispatcher; prefer reuse or document transient router usage.

## Reconciliation & Robustness
- Optional debounce / periodic repair loop to address missed events.
- Clear early guards in reconcile functions; unify ADD/UPDATE paths where feasible.

## Webhook/Validation
- Confirm CRD required fields (e.g., `status.tenants.*.pods`) and ensure operator fills minimal values.
- Extend webhook rules as Tenant expansion/migration logic evolves.

## Testing
- Extend resolver and orchestrator unit tests to cover indexers and event routing.
- Add daemon-side tests for NodeStore handler diffing (ignoring pods) and peering enqueue.

## Yesterday’s Items Recap (to keep in view)
- Orchestrator indexers fix (as above).
- Implement dispatcher `EnsurePeer`/`RemovePeer` triggered by remote NodeStore updates.
- Populate bridge IP/MAC in NodeStore status from NM snapshot (optional, but useful).
- Router reuse of the same NM instance to avoid duplicate state (cleanup).
- Optional periodic repair reconcile.

> Note: Keep patches small and idempotent; patch only what changed to reduce write pressure, and never write to remote NodeStores from a node-local daemon.
