# Handoff Prompt

Use this prompt in the new repo to continue the operator-runtime extraction without losing context:

```text
I am building a reusable Go operator runtime package extracted from a previous project. The previous package was a small event-aware controller helper based on `client-go` informers, a typed workqueue, and a router keyed by `(source, event)`.

I do not want the new design to be tied to Kubernetes informers only. I want the runtime to support multiple event source types through adapters, including:

- Kubernetes shared informers
- dynamic informers
- raw watches
- Go channels
- timers / periodic repair loops
- Kafka
- RabbitMQ
- HTTP / HTTPS
- gRPC
- Unix domain sockets
- other external transports later

The target architecture should be:

`Source Adapter -> Event -> Mapper -> Scheduler/Queue -> Reconciler -> Result/Ack`

Important design decisions already made:

1. The core runtime should not expose concrete `client-go` informer types in its public API.
2. The package should queue normalized reconcile `Request`s, not raw source events.
3. A normalized `Event` is produced by adapters and contains provenance plus optional metadata/payload.
4. A `Mapper` converts one `Event` into zero, one, or many `Request`s.
5. Reconciliation should be chosen from request metadata, usually via:
   - one reconciler per operator, or
   - a `ReconcilerRegistry` keyed by `Request.Class`
6. The old router idea is still useful, but mostly at the ingress/mapping edge:
   - event router or mapper registry before the queue
   - reconciler registry after the queue
7. Ack / commit semantics for Kafka, RabbitMQ, etc. should be separate from business reconciliation.
8. The runtime should remain event-aware, but reconciliation should be centered on converging current state, not branching forever on raw add/update/delete origin.

The old design had these strengths:

- simple event-aware queue
- explicit source and event provenance
- external emitters via channels and timers

The old design had these weaknesses:

- core API tied to `client-go` informers
- Setera-specific helpers in the package
- queue item was `source + event + resource` instead of a normalized request
- router-centric reconciliation encourages event-branching and duplicated logic

What I want next:

- propose the minimal core interfaces and types for the new package
- design a clean package layout separating:
  - core runtime
  - adapters
  - mappers
  - scheduler
  - reconciler registry
- explain tradeoffs before implementing
- then implement the first minimal version of the runtime

Constraints:

- Keep the core package transport-agnostic.
- Preserve the ability to attach non-Kubernetes event sources.
- Prefer pragmatic, small, extensible interfaces.
- Avoid premature scaffolding or CLI generation.
- Focus first on a strong reusable runtime contract.

Before coding, read any design docs in the repo and reconcile them with this target architecture.
```

Also carry this short summary forward:

- queue `Request`s, not raw events
- adapters normalize transport input into `Event`
- mappers translate `Event -> []Request`
- scheduler owns dedupe/retry/ordering
- reconciler chosen from request metadata
- router moves to the edges; it should not dominate the business model

If there is an existing package in the new repo, compare it against this target before refactoring.
