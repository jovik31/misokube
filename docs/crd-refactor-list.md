# CRD Refactor List

This list tracks the code refactors required by the current CRD shape changes.

## CRD Changes Observed

- `NodeStore.status.tenants[*]` was reduced to tenant-local pod membership only:
  - Kept: `name`, `pods`.
  - Removed: `tenant_cidr`, `vni`, `vtep_name`, `vtep_ip`, `vtep_mac`, `bridge_name`, `bridge_ip`, `bridge_mac`.
- `NodeStore.status.tunnelInfo` was added as node-level tunnel state:
  - `vtep_name`
  - `vtep_ip`
  - `vtep_mac`
- `NodeStore.status.freeSubnets` and `NodeStore.status.totalSubnets` were removed.
- `Tenant.status.assignedNodes[*].tenant network` was removed.
- `Tenant.status.assignedNodes[*]` still carries node-level VTEP identity:
  - `name`
  - `node ip`
  - `vtepIP`
  - `vtepMAC`

## Required Refactors

1. Refactor daemon NodeStore mirroring from NM snapshots.
   - Update `internal/daemon/reconcile_nm_source.go` so `TenantInfra` only receives `Name` and `Pods`.
   - Populate `NodeStore.status.tunnelInfo` once per node from the NM snapshot or a node-level NM accessor.
   - Remove all writes to deleted `TenantInfra` fields: `TenantCIDR`, `VNI`, `VTEP_NAME`, `VTEP_IP`, `VTEP_MAC`, `BRIDGE_NAME`, `BRIDGE_IP`, `BRIDGE_MAC`.
   - Remove all writes to deleted `NodeStoreStatus` fields: `FreeSubnets`, `TotalSubnets`.

2. Replace per-tenant peer metadata reads with the new node-level tunnel model.
   - Update `internal/daemon/reconcile_nodestore_source.go`.
   - `buildRemoteTenantInfra` can no longer derive `TenantCIDR`, `VNI`, or VTEP data from `TenantInfra`.
   - Use `ns.Status.TunnelInfo` for remote VTEP IP/MAC.
   - Decide the new source of tenant subnet and VNI before preserving `EnsurePeer`; those values are still required by `nmanager.RemoteTenantInfra`.

3. Refactor daemon NodeStore event diffing.
   - Update `internal/daemon/nodestore_event_handlers.go`.
   - Diff peer-relevant changes against `NodeStore.status.tunnelInfo` and tenant set membership.
   - Stop comparing removed per-tenant topology fields.
   - Keep pod-list comparisons separate because pod-only changes should not trigger peer topology reconciliation unless intended.

4. Refactor orchestrator Tenant status projection.
   - Update `internal/orchestrator/reconcile_nodestore_source.go`.
   - Stop assigning removed `NodeInfo.TenantCIDR`.
   - Populate `NodeInfo.VtepIP` and `NodeInfo.VtepMAC` from `NodeStore.status.tunnelInfo`, not `TenantInfra`.
   - Keep `NodeInfo.Name` and `NodeInfo.NodeIP` sourced from `NodeStore.spec`.

5. Replace subnet-count scoring.
   - Update `internal/orchestrator/tenant.go`.
   - `getNodeScoreFreeSubnets` currently depends on removed `NodeStore.status.freeSubnets` and `totalSubnets`.
   - Either remove this score from placement, compute capacity from `NodeStore.spec.nodeIP` plus Kubernetes node PodCIDR data, or introduce a new explicit CRD field for capacity if the API should still expose it.

6. Remove NodeStore subnet counter initialization.
   - Update `cmd/daemon/main.go` and `cmd/stub-daemon/main.go`.
   - Stop initializing or updating `Status.TotalSubnets` and `Status.FreeSubnets`.
   - Keep any internal NM capacity calculation inside NM/orchestrator scoring code instead of writing removed CRD fields.

7. Refresh generated API clients and CRD manifests after source refactors.
   - Regenerate deepcopy/applyconfiguration/clientset/informer/lister code after the API source is final.
   - Regenerate `config/crd/bases/*.yaml`.
   - Verify generated files do not reintroduce removed fields.

8. Update docs and examples that describe old NodeStore topology layout.
   - Update `README.md`, `docs/architecture-overview.md`, and `docs/TODO.md`.
   - Replace examples that describe per-tenant VTEP/bridge fields with node-level `status.tunnelInfo`.
   - Remove or redesign references to NodeStore subnet counters.

9. Update tests around tenant assignment, NodeStore reconcile, and peer setup.
   - Adjust orchestrator tests to remove expected `TenantCIDR`.
   - Add coverage that VTEP information is copied from `NodeStore.status.tunnelInfo` into `Tenant.status.assignedNodes`.
   - Add daemon tests for NodeStore diffing based on tunnel info and tenant membership.

## Open Design Decisions

- Where should tenant subnet and VNI live now?
  `nmanager.RemoteTenantInfra` still requires both, but the CRD change removes them from `NodeStore.status.tenants[*]`.
- Should subnet capacity be observable in any CRD?
  Placement currently uses free/total subnet counters from NodeStore status, but those fields were removed.
- Is bridge metadata still needed externally?
  It was removed from the CRD, so any remaining bridge-dependent reconciliation should use local NM state only.
