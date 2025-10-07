Setera Networking: Conversation Context (Design + Decisions)

Last updated: 2025-10-06

TL;DR

We designed a high-throughput, per-tenant–serialized path for CNI requests:

CNI Plugin (invoked by kubelet) → CNI UDS Server (Daemon Pod) over Unix Domain Socket using a small, framed binary transport (not HTTP/gRPC).

Resolver maps (ns, pod, uid) → tenantID using a snapshot + informer store hybrid for speed & precise errors.

Router forwards the request to the Tenant Actor via a mailbox (chan) and includes a per-request reply channel for responses.

Tenant Actor executes per-tenant network/IPAM work, replies on that channel.

CNI UDS Server prints CNI result to stdout for kubelet.

The system supports large parallel bursts, maintains per-tenant ordering, and offers clean timeouts, backpressure, and observability.

Components & Workflow
High-level flow

CNI Plugin → CNI UDS Server (Daemon)

UDS framed message: {Kind, Namespace, Pod, UID, ContainerID, IfName, NetNS, RawCNI, TraceID, Timestamp}.

CNI UDS Server → Resolver

Resolve(ns, pod, uid) → tenantID or retryable error (ErrNotReady, ErrNotIndexed, ErrNoTenantLabel).

CNI UDS Server → Router

Build PodOp with ReplyTo (chan Response), call Route(ctx, op).

Router → Tenant Actor

Lookup tenantID → TenantHandle{Mailbox}; enqueue op to mailbox (per-tenant serialization).

Tenant Actor → CNI UDS Server

Do work (IPAM, links, routes, policies); non-blocking send on op.ReplyTo.

CNI UDS Server → Kubelet

On response: print CNI ResultJSON; on timeout/error: return CNI error (retryable when appropriate).

Why reply channels?

Per request lifecycle with clean timeouts and no global correlation maps.

Router remains a lightweight address book + forwarder; responses bypass it.

Go-idiomatic; fewer moving parts than callbacks/buses/RPC.

Transport & UDS Server

Transport: tiny framed protocol (header with magic, version, type, length; payload = encoded JSON with pluggable codec).

Why not gRPC/HTTP over UDS? Custom framing is leaner, avoids HTTP/gRPC overhead, and fits single in-process hop.

Server: generic SocketServer (unix listener) with:

Max connections (semaphore), read/write timeouts, graceful shutdown.

Concurrency via per-conn goroutines.

Separation of wire (framing) from server (I/O & concurrency).

Permissions: sock path (e.g. /var/run/setera/cni.sock) created with strict perms (0600), validated on start.

Resolver (Pod → Tenant)
Design

Event-driven via informers (no busy loop).

Hot path: immutable snapshot (maps: uid→tenant, ns/pod→tenant) stored in atomic.Value for lock-free reads.

Miss classification via informer cache (GetStore().GetByKey, index for UID) to distinguish:

ErrNotReady — initial sync not done.

ErrNotIndexed — pod not in store yet (transient).

ErrNoTenantLabel — pod seen but missing tenant label (transient).

Node scoping: only index pods for the current node if configured (optional).

Start/Bootstrap (resolver-only example)

Build k8s client + shared informer factory.

resolver.Start(ctx) registers handlers.

factory.Start(ctx.Done()) runs the controllers.

WaitForCacheSync gates readiness.

Errors (full suite)

ErrNotReady, ErrNotIndexed, ErrNoTenantLabel (+ contextual wrappers with ns/pod/uid).

Router & Tenant Actors
Router

Threadsafely maintains: map[string]*TenantHandle{ID, Mailbox, Gen?}.

Route(ctx, op):

Lookup tenant; enqueue to Mailbox with bounded timeout.

Returns fast errors: ErrNoSuchTenant, ErrRouteTimeout.

No response correlation or state; reply path goes directly to the caller via ReplyTo.

Tenant Actor (per tenant)

Single goroutine + buffered mailbox (e.g., 256–1024) → per-tenant serialization and backpressure.

Processes PodOp (ADD/DEL/CHECK...), executes IPAM+net ops, returns Response {OK, Err, ResultJSON, RetryAfter} via ReplyTo.

Non-blocking send to avoid leaks if caller timed out.

Concurrency & Backpressure

Parallel CNI requests across tenants; serialized within the same tenant.

Router send timeout signals overload (mailbox full) → CNI returns retryable error.

CNI server waits on replyCh with ctx deadline → clean timeouts.

Testing
Unit tests (highlights)

Resolver:

Ready gate, snapshot updates, error classification.

Concurrency tests with preseeded pods (deterministic).

Watch-race tests allow transient ErrNotIndexed then success.

Server/Transport:

Header encode/decode, size limits (ErrPayloadTooLarge), magic mismatch (ErrMagic).

Permission checks for sock path.

Benchmarks

Hot path (snapshot hits): by UID & by name, sequential & parallel.

Miss paths: ErrNotIndexed, ErrNoTenantLabel.

Fix flakes by:

Preseeding before starting informers.

Warm-up loop to ensure every pod resolves once before timing.

CI Pipelines (GitHub Actions)

Lint & Security (security.yml):

golangci-lint, go vet, staticcheck.

SCA: govulncheck, trivy.

SAST: semgrep.

Secrets scan: gitleaks.

Optional DAST: OWASP ZAP (enable via repo var ZAP_TARGET_URL).

Unit tests (tests.yml):

go test ./... -race -covermode=atomic -coverprofile=....

Artifact: coverage.out.

Optional Codecov upload.

Triggered on push, pull_request, or workflow_dispatch.

AI-Assisted PR/Commit Messages

Workflow ai-pr-writer.yml + script ai_pr_writer.py:

Collect diffs and commit subjects, call OpenAI (model e.g. gpt-4o-mini).

Proposes Conventional Commit title, full commit message, and PR body.

Updates PR title/body and comments suggestion.

Requires repo secret OPENAI_API_KEY.

Daemon Bootstrap (Integration Idea)

Construct instances in main and inject dependencies:

Router (shared instance).

Resolver (pod informer).

TenantSupervisor (watches Tenants; creates actors; registers in Router).

CNIServer (has Resolver + Router + UDS server).

Start order:

Start informers; wait for resolver sync (optional hard gate).

Start supervisor (register actors).

Start CNI UDS server (accept loop).

All components shut down via context cancellation.