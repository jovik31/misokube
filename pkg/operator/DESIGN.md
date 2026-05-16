# Operator Runtime Design Note

This note captures the intended direction for extracting `pkg/operator` into a reusable operator runtime that is not bound to Setera, `client-go` informers, or Kubernetes-only event sources.

## Current State

Today `pkg/operator` is a small event-aware controller helper built around:

- `BaseOperator` with a typed rate-limiting queue
- `EventReconciler` with `ReconcileEvent(ctx, src, ev, res)`
- informer registration helpers for `client-go`
- predicates and enqueue helpers
- channel/timer emitters
- a router keyed by `Source` and `Event`

This is useful, but it is still too tightly coupled to:

- `client-go` informer types in the core API
- Setera-specific helpers in `util.go`
- a raw event queue model instead of a normalized reconcile request model

## Goals

The target package should:

- support many source types through adapters
- normalize transport-specific input before it reaches reconciliation
- keep reconciliation centered on convergence of current state
- preserve source provenance when needed
- remain useful for Kubernetes controllers and non-Kubernetes control planes

Potential source types:

- Kubernetes shared informers
- dynamic informers
- raw watches
- Go channels
- timers / periodic repair loops
- Kafka
- RabbitMQ
- NATS / JetStream
- HTTP / HTTPS
- gRPC
- Unix domain sockets
- WebSockets / SSE
- file watchers
- DB / CDC streams

## Core Architecture

The runtime should follow this pipeline:

`Source Adapter -> Event -> Mapper -> Scheduler/Queue -> Reconciler -> Result/Ack`

### 1. Source Adapter

Each adapter wraps one transport or event provider and emits normalized events.

Examples:

- `adapter/clientgo/informer`
- `adapter/clientgo/watch`
- `adapter/channel`
- `adapter/timer`
- `adapter/http`
- `adapter/grpc`
- `adapter/kafka`
- `adapter/rabbitmq`

The core package should not know whether an event came from an informer, a topic, a webhook, or a socket.

### 2. Event

An `Event` is a normalized ingress envelope. It should carry provenance and optional transport metadata.

Suggested fields:

- `Source`
- `Type`
- `Subject` or `ObjectRef`
- `OccurredAt`
- `Metadata`
- `Payload`
- `CorrelationID`
- optional ack / commit handle for durable transports

An event is not the queued work item. It is the raw normalized input to mapping.

### 3. Mapper

A `Mapper` converts one `Event` into zero, one, or many `Request`s.

This is where the equivalent of watch handlers and predicates belongs:

- filter events
- map one event to one request
- map one event to many requests
- perform owner-like mapping
- perform fan-out
- translate non-Kubernetes payloads into reconciliation targets

This is also the right place for an event router if one is needed.

### 4. Request

A `Request` is the canonical unit of work for the scheduler and reconciler.

It should describe what needs convergence, not what transport emitted the signal.

Suggested fields:

- `Class`
- `Key`
- `Namespace`
- `Name`
- `Kind`
- `Priority`
- `PartitionKey`
- optional `Cause`

Multiple very different sources should be able to map to the same request.

Examples:

- informer update for a `Tenant`
- Kafka event saying a tenant changed
- HTTP repair request
- periodic repair timer

All can become:

- `Request{Class: "tenant", Key: "ns/name"}`

### 5. Scheduler / Queue

The scheduler should expose one logical queue API, but it should queue normalized `Request`s, not raw events.

Default behavior:

- dedupe by request key
- rate limit retries
- support delayed requeue
- support worker concurrency

Possible future behavior:

- priority queues
- per-source admission control
- per-partition ordering
- per-key serialization

Do not keep the current model of queueing raw `Source + Event + Resource` items as the primary abstraction once multiple transports are introduced.

### 6. Reconciler

Reconciliation should be chosen from request metadata, not directly from the raw source event.

Recommended shape:

- either one reconciler per operator
- or a reconciler registry keyed by `Request.Class`

This is the main replacement for the current router-centric flow.

### 7. Result / Ack

The reconciler returns a result such as:

- success
- requeue immediately
- requeue after delay
- permanent failure

Ack and commit semantics for durable transports should be handled separately from business reconciliation.

Examples:

- Kafka offset commit on successful reconcile
- RabbitMQ ack / nack
- HTTP response `202 Accepted` before async processing

## Routing Model

The current package routes via:

- `Source`
- `Event`

That approach is useful, but it should move to the edges of the system.

Recommended split:

- `EventRouter` or mapper registry before the queue
- `ReconcilerRegistry` after the queue

This keeps transport and ingress concerns separate from convergence logic.

### Why Not Keep the Existing Router as the Main Model

Advantages of the current router:

- easy to understand
- natural for heterogeneous inputs
- preserves explicit source/event provenance
- useful for daemon-style systems

Disadvantages:

- encourages branching by add/update/delete instead of converging current state
- can duplicate logic across events
- makes it harder to coalesce many signals into one reconcile
- couples business logic to trigger origin
- gets weaker as transports diversify

Conclusion:

- router at the ingress and mapping boundary is good
- router as the main reconciliation model is limiting

## Recommended Package Shape

Possible layout:

- `pkg/operator`
  - core runtime interfaces and scheduler
- `pkg/operator/adapter/clientgo/informer`
- `pkg/operator/adapter/clientgo/watch`
- `pkg/operator/adapter/channel`
- `pkg/operator/adapter/timer`
- `pkg/operator/adapter/http`
- `pkg/operator/adapter/grpc`
- `pkg/operator/adapter/kafka`
- `pkg/operator/adapter/rabbitmq`

Keep the core package free of:

- Setera API imports
- concrete informer types in public APIs
- transport-specific retry or ack behavior

## Proposed Minimal Interfaces

High level only:

```go
type Event struct {
    Source        string
    Type          string
    Class         string
    Namespace     string
    Name          string
    Key           string
    Metadata      map[string]string
    CorrelationID string
    Payload       any
    Cause         any
}

type Request struct {
    Class        string
    Key          string
    Namespace    string
    Name         string
    Kind         string
    Priority     int
    PartitionKey string
    Cause        any
}

type Result struct {
    Requeue      bool
    RequeueAfter time.Duration
}

type Sink interface {
    Emit(context.Context, Event) error
}

type Source interface {
    Name() string
    Start(context.Context, Sink) error
    HasSynced() bool
}

type Mapper interface {
    Map(context.Context, Event) ([]Request, error)
}

type Reconciler interface {
    Reconcile(context.Context, Request) (Result, error)
}

type Registry interface {
    Get(Request) (Reconciler, error)
}
```

The exact API can change, but the separation of concerns should remain.

## Flow Examples

### Kubernetes Informer

1. Informer adapter receives update
2. Adapter emits normalized `Event`
3. Mapper evaluates predicates and maps to `Request{Class: "tenant", Key: "ns/name"}`
4. Scheduler dedupes bursts
5. Tenant reconciler reads current state and converges

### Kafka

1. Kafka adapter receives message
2. Adapter emits event with source metadata and commit handle
3. Mapper translates payload to one or more requests
4. Scheduler may serialize by partition key
5. Reconciler converges
6. Ack manager commits offset on success

### HTTP

1. HTTP adapter receives request
2. Adapter validates and emits event
3. Mapper emits request
4. Runtime queues async work
5. Endpoint returns `202`
6. Reconciler runs separately

### Go Channel

1. Channel adapter receives value
2. Adapter emits event
3. Mapper emits request
4. Normal reconcile path continues

## Migration Plan

Recommended extraction order:

1. Remove Setera-specific helpers from `pkg/operator`
2. Introduce normalized `Event`, `Request`, and `Result`
3. Introduce `Mapper` and `ReconcilerRegistry`
4. Replace informer-specific registration in the core with `Source`
5. Move current informer helpers into a `client-go` adapter package
6. Keep channel/timer helpers as adapters instead of special cases
7. Add tests for mapping, scheduling, retries, and source startup

Do not start with scaffolding or CLI generation. Stabilize the runtime contract first.

## Design Rules

- queue requests, not raw transport events
- normalize transport input before scheduling
- preserve provenance, but do not make transport the main reconciliation key
- adapters own transports
- mappers own event-to-request translation
- scheduler owns execution policy
- reconcilers own convergence
- ack/commit logic stays outside business reconcile

## Summary

The reusable package should become a generic event-to-reconcile runtime:

- many sources
- normalized ingress
- mapper-based translation
- request-based scheduling
- request-class-based reconciler selection
- transport-specific adapters at the edges

That keeps the flexibility of the current event-aware design while avoiding tight coupling to `client-go`, Setera CRDs, or a router-only control model.
