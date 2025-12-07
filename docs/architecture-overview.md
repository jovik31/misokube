# Setera Orchestrator: Cluster Architecture Overview

This document summarizes the multi-component design for orchestrating tenant networking across the cluster. It distinguishes responsibilities between the single Orchestrator (cluster-scoped) and Daemons (node-scoped), and explains data/control flows, operators, actors, and runtime services.

## Components

### Orchestrator (singleton)
- **Tenant Operator (writer):**
  - Owns `Tenant` CRs
  - Assigns/updates `assignedNodes` for tenants
  - Drives expansion/migration phases via status
- **NodeStore Reader (reader-only):**
  - Watches all `NodeStore` CRs
  - Enqueues tenant reconciliation to aggregate node-derived state
  - Propagates summaries back to `Tenant.status` (no node-local writes)

### Daemon (one per node)
- **NodeStore Operator (writer for local node):**
  - Owns local node’s `NodeStore` CR
  - Reads remote nodes’ `NodeStore` updates
  - Executes NodeStore Ops via Network Manager to establish communication to remote nodes (ARP/FDB/Route wiring)
- **Tenant Operator (reader):**
  - Watches `Tenant` CRs
  - If local node is assigned, enqueues Network Manager to ensure/remove tenant infra
- **Network Manager:**
  - Owns per-node networking devices and per-tenant infra (bridge, VTEP, IPAM, iptables)
  - Provides Pod Ops (`AllocatePod`, `RemovePod`) and NodeStore Ops (peer wiring)
  - Manages per-tenant actors (mailbox goroutines) for serialized, per-tenant pod operations
  - Emits NodeStore reconciliation updates reflecting local infra state
- **Dispatcher:**
  - Bridges Tenant Operator (read) to Network Manager’s `EnsureTenant` / `RemoveTenant`
  - Processes tenant lifecycle commands (idempotent)
- **CNI Server:**
  - Handles CNI `ADD/DEL/CHECK/STATUS` over UDS
  - Delegates pod operations through the Router after tenant resolution
- **Resolver (pods):**
  - Pod informer + UID indexer
  - Resolves `tenantID` for `podUID` using annotations with default fallback
- **Router (pod ops):**
  - Bridges CNIServer to Network Manager via per-tenant actors
  - Enqueues `AllocatePod`/`RemovePod` messages and awaits per-request replies (timeout-aware)

## Data and Control Flows

### Tenant Lifecycle (Orchestrator → Daemons)
1. Orchestrator’s Tenant Operator updates `Tenant` CR desired state (including `assignedNodes`).
2. Daemon’s Tenant Operator detects that local node is assigned and enqueues `EnsureTenant` via Dispatcher.
3. Network Manager `EnsureTenant`:
   - Allocates subnet (Trie)
   - Creates backend devices (bridge + vxlan)
   - Initializes IPAM and iptables
   - Starts and registers the per-tenant actor
4. Daemon’s NodeStore Operator updates local `NodeStore` status with current infra (CIDR, VTEP/bridge phases, IPAM capacity/usage).
5. Orchestrator’s NodeStore Reader aggregates node states and reflects summaries back into `Tenant.status` as needed.
6. `RemoveTenant` tears down infra, stops actor, and cleans NodeStore state.

### Pod Networking (CNI path)
1. CNIServer receives CNI `ADD` and resolves `tenantID` via Resolver.
2. Router looks up the tenant actor and enqueues `AllocatePod(epKey)` with a reply channel.
3. Tenant Actor loop invokes `NetworkManager.AllocatePod(tenantID, epKey)`:
   - IPAM: allocate IP
   - Backend: create veth, move peer to pod ns, attach host end to bridge/VTEP
   - Route/ARP/FDB: program per-pod and peer entries as needed
   - IPTables: apply tenant chain rules (if required)
4. Actor replies on the channel; Router converts to CNI `current.Result` and CNIServer returns the response.
5. CNI `DEL` follows the same path using `RemovePod` to detach and release resources.

## Actor Model
- **Per-Tenant Actor:**
  - Goroutine with a buffered mailbox; serializes tenant-specific pod ops
  - Processes `AllocatePod`, `RemovePod`, and `Stop` messages
  - Calls Network Manager methods; does not own networking logic
  - Returns per-request results via reply channels (buffered, timeout-aware)

## Concurrency and Ordering
- **Across Tenants:** independent actors enable parallel operations
- **Within a Tenant:** mailbox enforces strict ordering to avoid races on shared infra
- **CNIServer:** handles multiple connections concurrently, each awaiting its own actor reply

## Responsibilities Summary
- **Orchestrator:** cluster-wide desired state, tenant placement, aggregate status
- **Daemon:** node-local infra ownership, pod attachment, peer wiring, NodeStore ownership
- **Dispatcher:** tenant lifecycle bridge from operator to manager
- **Network Manager:** networking device/IPAM orchestration and per-tenant actors
- **Resolver:** pod UID → tenant resolution via informer indexer
- **Router:** CNIServer ↔ tenant actor bridge for Pod Ops
- **CNI Server:** protocol handling; relies on Router + Resolver

## Implementation Notes
- Prefer idempotent operations and small patches to CRDs
- Actor Stop: enqueue `Stop`, cancel loop context, remove from `TenantActors`
- Router should return retryable errors when actor missing or mailbox backpressure occurs
- Resolver: gate on informer sync; default tenant when annotation missing

## Future Enhancements
- Enrich Router responses with full CNI current.Result (IP, gateway, routes, DNS)
- Metrics for mailbox depth, op latency, and error rates
- Optional sharded actors for high-traffic tenants (preserve ordering guarantees where needed)
