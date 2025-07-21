# Setera
A tenant orchestrator for k8s environments that ensures network isolation between tenants. 

IN ACTIVE DEVELOPMENT

The setera project is an upgrade of the following project [TenantCNI](https://github.com/jovik31/TenantCNI).




Tenant Multi‑Node Orchestrated Networking – Deep Dive Design
Version: 1.0
Date: 2025‑07‑19
Author: (You + AI assistant consolidation)

Table of Contents
Executive Summary

System Overview

Core Components

Orchestrator Process

Tenant Operator (Cluster‑Scoped Controller)

NodeStore Operator (Daemon Local Controller)

Daemon Network Manager

Custom Resource Definitions

Tenant CRD

NodeStore CRD

Networking Architecture

IP Allocation Strategy (Trie + IPAM Bitmap)

Subnet Expansion Logic

VXLAN / VTEP Management

Bridge (Gateway) Management

Pod Attachment (veth, namespace configuration)

Acknowledgement & Migration Model

Minimal Overhead Ack Summary

VTEP Migration State Machine

Bridge (Gateway) Dual‑IP Migration

Controllers Reconciliation Flows

Tenant Add / Initial Placement

Tenant Update (Expansion / Migration)

NodeStore Reconcile (Local)

Automatic Subnet Expansion Trigger

Active Connection Preservation

Status & Conditions Design

Security & RBAC Considerations

Performance Considerations

Observability & Metrics

Failure Modes & Recovery

Runbooks

Subnet Expansion Runbook

VTEP Migration Runbook

Rollback Procedure

Open Questions / Future Enhancements

Selected Code Snippets

Glossary

1. Executive Summary
This document defines a multi‑node tenant networking system for Kubernetes involving:

A global orchestrator selecting nodes for tenant deployment based on score.

A Tenant CRD representing desired multi‑node placement + subnet sizing.

Daemon agents (per node) that create local network constructs (bridge, vxlan) and manage pod IP assignment via a trie‑backed expandable subnet using a compact IPAM bitmap.

A NodeStore CRD per node capturing local tenant networking state (including migration phases and acknowledgement summaries).

Controlled subnet expansion (power‑of‑two merges upward in an IP trie) triggered by IP exhaustion, with optional dual‑IP gateway migration and coordinated VTEP migration minimizing traffic disruption.

2. System Overview
High‑level flow:

User creates a Tenant specifying desired nodes count (zones) & (optionally) target prefix size.

Orchestrator collects node scores, assigns top N nodes, annotates Tenant status → AssignedNodes.

Each selected node’s daemon sees Tenant reference, creates local network constructs and updates its NodeStore.

Orchestrator watches NodeStores to confirm readiness; populates Tenant status with VTEP MAC/IP per node.

Pods scheduled for tenant leverage the CNI plugin querying the daemon for tenant network info (bridge name, vxlan, IPAM allocation).

As tenant address space exhausts, daemon triggers a controlled trie merge expansion, updating cluster state and potentially migrating VTEP / gateway with acknowledgements.

3. Core Components
3.1 Orchestrator Process
Runs out‑of‑cluster or in cluster.

Holds NodeScoreCache from daemon reports.

Runs TenantOperator to reconcile Tenant lifecycle:

Add finalizer.

Assign nodes.

Monitor NodeStore acknowledgements.

Drive expansion finalization.

3.2 Tenant Operator (Cluster‑Scoped Controller)
Responsibilities:

Event handlers (Add / Update / Delete).

Node scoring & selection.

Subnet expansion decision initiation (mark status Expanding & call trie merge).

VTEP migration finalization when all node acks aggregated.

Removal logic respecting finalizers.

3.3 NodeStore Operator (Daemon Local Controller)
Runs inside daemon pod; reconciles only local node’s NodeStore.

Observes Tenants; when local node is assigned:

Ensures network constructs (bridge, vxlan) exist.

Allocates / updates IPAM.

Performs expansion / migration tasks.

Sets SelfAck and computes local PeerSummary (derived global view).

3.4 Daemon Network Manager
Encapsulates low-level netlink operations.

Owns IPAM map (tenantID → IPAM).

Provides:

EnsureTenantNetwork()

AllocatePodIP(containerID)

ExpandTenantSubnet(tenantID)

EnsureVxlan (idempotent)

SetupBridge

Pod veth setup / teardown

Persists state (optional) via local JSON for crash recovery (future).

4. Custom Resource Definitions
4.1 Tenant CRD (Spec & Status Sketch)
Spec (essential fields):

yaml
Copy
Edit
spec:
  zones: 3                      # number of nodes to place
  desiredPrefix: 29             # target network prefix (optional)
Status (key fields):

yaml
Copy
Edit
status:
  assignedNodes: ["nodeA","nodeB","nodeC"]
  allocatedPrefix: 30
  expansionPhase: "" | "Expanding" | "Expanded" | "Blocked"
  expansionReason: ""
  vtepPhase: "" | "Migrating" | "Finalizing"
  # (Optionally) list of per-node VTEP endpoints summarized
  conditions:
    - type: Ready
      status: "True"
      reason: ...
4.2 NodeStore CRD (Per-Node)
Per node’s perspective of all tenants resident:

yaml
Copy
Edit
status:
  tenants:
    - tenantID: t1
      networkCIDR: 10.0.0.0/29
      ipam:
        allocatedIPs: 37
        capacityIPs: 62
      vtep:
        phase: "Migrating"
        old:
          ip: 10.0.0.5
          mac: aa:bb:...:ff
          vni: 123456
        current:
          ip: 10.0.0.1
          mac: aa:bb:...:11
          vni: 123456
        selfAck: true
        peerSummary:
          totalNodes: 3
          ackedNodes: 2
          pending: ["nodeC"]
        since: 2025-07-19T12:34:56Z
        error: ""
      bridge:
        phase: "Dual"
        oldIP: 10.0.0.6
        currentIP: 10.0.0.2
        selfGatewayMigrated: true
        podsPending: 1
Minimal overhead ack summary is peerSummary instead of a full boolean map.

5. Networking Architecture
5.1 IP Allocation Strategy (Trie + IPAM Bitmap)
Global Trunk CIDR (root of trie) subdivided recursively down to /30 leaves (or chosen smallest unit).

Allocation chooses free leaf maximizing expandability depth (greedy expansion potential).

IPAM (per tenant):

Tracks only pod host slots (excluding network & bridge reserved).

Bitmap (uint64 slices) for compactness.

Index → IP mapping: podIndex + reservedHostCount + base.

Supports ExpandToParent() doubling address space and remapping indices.

5.2 Subnet Expansion Logic
Trigger: IP exhaustion (allocation returns ErrIPExhausted) or spec desiredPrefix < current.

Controller merges trie node with free sibling → parent.

IPAM expands (copy & offset).

Optionally migrate gateway & VTEP.

Status phases:

Tenant: Expanding → (all nodes ack) → Expanded

NodeStore: local per-tenant vtep / bridge phases.

5.3 VXLAN / VTEP Management
EnsureVxlan: idempotent creation with deterministic VNI from tenant hash (24-bit) & deterministic MAC from (tenant|node).

Optional relocation of VTEP IP (parent base) during expansion. (Recommended to avoid moving unless strongly needed.)

Learning disabled (Learning=false) if relying on controller-driven FDB management.

5.4 Bridge (Gateway) Management
Bridge per tenant: br-<hash(tenant)>.

Gateway IP initially = first host of leaf subnet.

Dual‑IP migration during expansion (add new first host of parent, keep old until pods switch).

If stability prioritized, can skip moving gateway (leave old IP).

5.5 Pod Attachment (veth, namespace configuration)
SetupVeth:

Create pair inside pod namespace.

Assign pod IP (/prefix or /32 + route).

Add default route via bridge (if model uses gateway).

On gateway migration:

Replace default route in each pod NS (or enlarge route).

Optionally update mask to new supernet.

6. Acknowledgement & Migration Model
6.1 Minimal Overhead Ack Summary
Each NodeStore contains self state + summary:

peerSummary.totalNodes

peerSummary.ackedNodes

peerSummary.pending[]

Derived locally by reading all NodeStores via indexer, not by writing to others.

Reduces etcd size vs N² maps.

6.2 VTEP Migration State Machine
Phase	Description	Exit Condition
(empty)	Normal	Expansion triggered
Migrating	Old+Current present; dual acceptance	All nodes SelfAck=true
Finalizing	Orchestrator signalled removal	Initiator removes old; nodes observe
(empty)	New stable (old cleared)	Old removed everywhere

6.3 Bridge (Gateway) Dual‑IP Migration
Phase	Description	Action
Single	Only old IP	Add new IP
Dual	Old + new	Update pod routes sequentially
Cutover	New only (old pending removal)	Remove old when pods migrated
Single (new)	Stable	Normal ops

7. Controllers Reconciliation Flows
7.1 Tenant Add / Initial Placement
Fetch tenant.

Add finalizer if absent.

If no nodes assigned:

Query NodeScoreCache top zones.

Set status.assignedNodes.

Initialize allocatedPrefix = leaf prefix.

Patch status & metadata (two patches if separating finalizer vs status).

7.2 Tenant Update (Expansion / Migration)
Compare desiredPrefix vs allocatedPrefix.

If expansion needed:

Call trie MergeSubnet.

Set expansionPhase=Expanding.

Wait until all NodeStores updated networkCIDR and (if migrating gateway/VTEP) peerSummary.pending=[].

Set expansionPhase=Expanded, update allocatedPrefix.

7.3 NodeStore Reconcile (Local)
For each relevant tenant:

If assigned & network absent → EnsureTenantNetwork().

If IP exhaustion → expand (update IPAM, networkCIDR).

If tenant status says finalizing VTEP and peerSummary.allAcked=true & node is initiator → remove old VTEP IP.

Recompute peerSummary if NodeStore events changed.

8. Automatic Subnet Expansion Trigger
Pseudo:

go
Copy
Edit
ip, err := ipam.AllocatePodIP(cid)
if errors.Is(err, ErrIPExhausted) {
    if canExpand := trie.CanExpand(tenantID); canExpand {
        triggerExpansion(tenantID)
        // retry allocate after expansion
    }
}
Pre‑allocation guard ensures single expansion at a time (per‑tenant mutex).

9. Active Connection Preservation
Dual‑IP bridge migration avoids sudden gateway loss.

Optional conntrack polling (10s idle window) prior to old IP removal:

Count active flows referencing old gateway or tenant pod IP pairs.

Grace period fallback if busy flows never drain.

10. Status & Conditions Design
Use conditions for user clarity:

Condition	Meaning
Ready	Tenant networking fully configured on all assigned nodes
ExpansionInProgress	expansionPhase=Expanding
ExpansionBlocked	No contiguous free sibling; reason set
MigrationInProgress	VTEP or gateway migration active

Each condition has status, reason, message, lastTransitionTime.

11. Security & RBAC Considerations
Daemon service account:

get,list,watch,update,patch on its own NodeStore.

get,list,watch on Tenants & NodeStores cluster-wide (read-only others).

Orchestrator:

Full read/write Tenant.

Read NodeStore.

Avoid letting nodes patch other NodeStores to prevent lateral tampering.

Validate tenant spec via admission webhook (future) for zones sanity & desiredPrefix bounds.

12. Performance Considerations
Aspect	Note
Trie operations	O(height) ~ small (max 32 for IPv4)
IPAM allocation	Scan words; optimize by keeping a ‘nextFreeWord’ hint
Expansion	Bitmap copy (linear in words). Infrequent vs allocation
Peer summary patches	Debounce + only patch on change
Conntrack polling	Limit frequency (e.g. every 3s) & filter by subnet

Memory per tenant: bitmap size = hosts / 64 * 8 bytes. For /24 without 2 reserved + broadcast ~ 253 hosts ⇒ 4 words ≈ 32 bytes.

13. Observability & Metrics
Suggested Prometheus metrics:

Metric	Type	Labels
tenant_allocated_prefix	Gauge	tenant
tenant_ipam_capacity	Gauge	tenant
tenant_ipam_used	Gauge	tenant
tenant_expansion_events_total	Counter	tenant, outcome
tenant_expansion_duration_seconds	Histogram	tenant
tenant_vtep_migration_phase	Gauge (enum)	tenant, node
tenant_active_conntrack_flows	Gauge	tenant, node
daemon_ip_alloc_latency_seconds	Histogram	tenant

Events:

Expansion start / complete / blocked.

VTEP migration start / finalize / timeout.

14. Failure Modes & Recovery
Failure	Detection	Recovery
Sibling allocated concurrently	Trie merge error	Retry; mark Blocked if persistent
NodeStore update conflict	Patch error (409)	Backoff & retry
Expansion partial (some nodes fail IPAM)	NodeStore error field	Retry reconcile; do not rollback trie (avoid fragmentation)
VTEP migration timeout	since + timeout exceeded	Rollback or force finalize with warning
Daemon crash	Missing NodeStore heartbeat (future)	Recreate daemon; rebuild from persisted state or network introspection

15. Runbooks
15.1 Subnet Expansion Runbook
Event “ExpansionInProgress”.

Watch NodeStores: confirm networkCIDR updated to parent.

Ensure expansionPhase switches to Expanded.

If Blocked > X mins, investigate free sibling occupancy.

15.2 VTEP Migration Runbook
Phase = Migrating.

Check peer summaries: pending reduces to zero.

Orchestrator moves to Finalizing.

Initiator removes old VTEP; verify remote FDB updated (ping across nodes).

Phase clears.

15.3 Rollback Procedure
If migration fails majority nodes:

Stop new pod allocations.

Reinstate old gateway/VTEP (if still present) by clearing current and moving old back.

Mark Tenant condition MigrationRolledBack=True.

Schedule diagnostic job.

16. Open Questions / Future Enhancements
Area	Question
Multi-subnet per tenant	Support disjoint additional leaf instead of contiguous expansion?
IPv6 dual‑stack	Mirror trie & IPAM for IPv6 (/64 segments)
FDB programming	Central vs distributed; aging policy
Persistence	Persist IPAM state to disk for crash recovery
Security	Validate that node cannot impersonate another NodeStore
QoS / weighting	Integrate node score adjustments after expansion load shifts

17. Selected Code Snippets
17.1 Deterministic VNI & MAC
go
Copy
Edit
func VNI(tenant string) int {
    sum := sha256.Sum256([]byte(tenant))
    v := int(binary.BigEndian.Uint32(sum[0:4]) & 0xFFFFFF)
    if v == 0 { v = 1 }
    return v
}

func VtepMAC(tenantID, nodeName string) net.HardwareAddr {
    sum := sha1.Sum([]byte(tenantID + "|" + nodeName))
    mac := make([]byte, 6)
    copy(mac, sum[:6])
    mac[0] = (mac[0] & 0xFE) | 0x02
    return mac
}
17.2 Ensure VXLAN (Simplified Core)
go
Copy
Edit
func EnsureVxlan(cfg VxlanEnsureConfig) (*netlink.Vxlan, error) {
    // validate, derive SrcIP & MTU
    // lookup existing link
    // reconcile immutables or recreate
    // set MAC, up, ensure /32 VTEP address, return
}
17.3 IPAM Expansion (Core Skeleton)
go
Copy
Edit
func (ipam *IPAM) ExpandToParent() (*net.IPNet, error) {
    ipam.mu.Lock()
    defer ipam.mu.Unlock()

    parent, err := ParentCIDR(ipam.Network)
    if err != nil { return nil, err }

    // derive offsets, allocate new bitmap ~ double capacity
    // copy old allocations with index shift
    ipam.Network = parent
    ipam.bitmap = newBitmap
    return parent, nil
}
17.4 Peer Summary Update
go
Copy
Edit
func (d *Daemon) updatePeerSummary(tenantID string) error {
    nss, _ := d.nodeStoreIndexer.ByIndex("tenantID", tenantID)
    total := len(nss)
    acked := 0
    pending := []string{}
    for _, o := range nss {
        ns := o.(*NodeStore)
        ts := findTenant(ns, tenantID)
        if ts == nil || ts.Vtep == nil || !ts.Vtep.SelfAck {
            pending = append(pending, ns.Name)
        } else {
            acked++
        }
    }
    my := d.localNodeStoreDeepCopy()
    mts := findTenant(my, tenantID)
    if mts != nil && (mts.Vtep.PeerSummary.TotalNodes != total ||
        mts.Vtep.PeerSummary.AckedNodes != acked ||
        !sliceEqual(mts.Vtep.PeerSummary.Pending, pending)) {
        mts.Vtep.PeerSummary.TotalNodes = total
        mts.Vtep.PeerSummary.AckedNodes = acked
        mts.Vtep.PeerSummary.Pending = pending
        patchNodeStoreStatus(my)
    }
    return nil
}
18. Glossary
Term	Definition
Tenant	Logical multi‑node grouping requiring isolated network space.
Tenant Operator	Controller reconciling Tenant CR: placement, expansion decisions.
NodeStore	Per-node CR recording local tenant network state & migration phases.
Trie (IPTrie)	Binary subdivision tree tracking allocated / free subnet blocks.
Leaf / Leaf Subnet	Smallest allocatable block (e.g. /30) before expansion merges upward.
Expand / Merge	Allocating a parent block after sibling free → larger tenant subnet (e.g. /30 → /29).
IPAM Bitmap	Compressed bitset tracking pod IP slot usage within tenant subnet.
VTEP (VXLAN Tunnel Endpoint)	Source node IP (and MAC) used for VXLAN encapsulation.
VXLAN Device	Linux netlink interface providing L2 overlay for tenant traffic.
Bridge (Gateway)	Linux bridge per tenant bridging pod veth interfaces; provides gateway IP.
Dual‑IP Migration	Transitional period where both old and new bridge IPs exist to allow graceful pod route updates.
SelfAck	Boolean: local node has successfully applied new network state (e.g. VTEP migration).
Peer Summary	Aggregated view (total / acked / pending) of other nodes’ self acknowledgements stored in each NodeStore.
Expansion Phase	Tenant status state machine: Expanding, Expanded, Blocked.
VTEP Migration Phase	Migrating, Finalizing, or empty (stable).
Reserved Hosts	Network + bridge initial addresses not allocatable to pods.
Encap Overhead	Packet size overhead added by outer L2/L3/UDP/VXLAN headers.
FDB Entry	Forwarding database entry mapping MAC to remote VTEP for VXLAN.
Conntrack	Kernel connection tracking table used to observe active flows.
ForceRecreate	EnsureVxlan option to delete & recreate device on immutable mismatch.

