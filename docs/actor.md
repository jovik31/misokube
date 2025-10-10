## Actor pattern

The idea behind the actor pattern is __concurency by composition__.
An actor is a lightweight and isolated entity that:
- processes a single message at a time
- has its own state
- communicates via asnychronous messaging

### Why the actor pattern

- __Isolation & fault-containment__: An actor failure does not affect the entire system. Its supervisors decide the recovery and error handling mechanisms.
- __Elastic concurrency__: Its a fit for work queues, routers, sharded services and per-tenant implementations.
- __Backpressure & flow control__: Mailbox sizes and routing control throughput explicitly.
- __Location transparency__: A PID/ActorRef can be local or remote.

### Components

### Actor
- 

## Tenant actor5