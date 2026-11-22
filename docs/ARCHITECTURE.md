# EchoStream — Architecture Deep Dive

> A multi-tenant, real-time **chat backend** written in Go — the API layer behind a Slack-style
> product. Tenants, users, channels, memberships, and messages are persisted in PostgreSQL; live
> message delivery uses **WebSockets** and **Redis pub/sub**. Typing and presence-change events
> currently stay within each server's local Hub.

These notes describe the current implementation and its limits. The [roadmap](#14-roadmap--what-to-build-next)
contains proposed work; those items are not implemented capabilities. This development project
has no published load-test capacity or production-readiness claim.

---

## Table of Contents

1. [System at a Glance](#1-system-at-a-glance)
2. [C4 Level 1 — System Context](#2-c4-level-1--system-context)
3. [C4 Level 2 — Container View](#3-c4-level-2--container-view)
4. [C4 Level 3 — Internal Components](#4-c4-level-3--internal-components)
5. [Data Model (ERD)](#5-data-model-erd)
6. [Layered Request Lifecycle — Send a Message](#6-layered-request-lifecycle--send-a-message)
7. [Real-Time Delivery — WebSockets + Redis Fan-out](#7-real-time-delivery--websockets--redis-fan-out)
8. [The Hub Event Loop](#8-the-hub-event-loop)
9. [Presence & Typing](#9-presence--typing)
10. [Cross-Cutting Concerns](#10-cross-cutting-concerns)
11. [Scaling & Capacity Planning](#11-scaling--capacity-planning)
12. [Failure Modes & Mitigations](#12-failure-modes--mitigations)
13. [Key Design Decisions & Tradeoffs](#13-key-design-decisions--tradeoffs)
14. [Roadmap — What to Build Next](#14-roadmap--what-to-build-next)
15. [Design Principles Summary](#15-design-principles-summary)

---

## 1. System at a Glance

EchoStream accepts chat messages over REST, stores them in PostgreSQL, and attempts live delivery
through Redis pub/sub and WebSockets. Persisted history and live delivery have separate failure modes.

| Concern | How EchoStream handles it |
|---|---|
| **Durability** | Every message is written to PostgreSQL *before* the sender is acknowledged. The DB is the single source of truth; real-time delivery is a best-effort layer on top. |
| **Tenant context** | JWT claims carry `(user_id, tenant_id, email)`. Channel and message-history reads filter by tenant; membership lookups use channel/user IDs. Private-channel history lacks a membership check. |
| **Real-time delivery** | A single Hub goroutine owns local connection state. Redis forwards message events to subscribed instances on a best-effort basis. |
| **Multiple instances** | Redis decouples message producers from local WebSocket connections. This message path can cross instances; typing and presence-change events currently cannot. |
| **Presence** | Redis keys (`presence:<userID>`) expire after 90 seconds and refresh every 30 seconds. The Hub's connection count is per instance, so disconnect handling is not globally coordinated. |
| **Fair usage** | Per-user fixed-window rate limiting in Redis; fails open on Redis outage. |
| **Ordering & pagination** | Messages use a monotonic `bigserial` ID for cheap ordering and cursor pagination. |
| **Graceful shutdown** | SIGINT/SIGTERM drains in-flight HTTP requests, then tears down pubsub → hub → redis → postgres → logger in order. |

**Tech stack:** Go 1.25 · Gin (HTTP) · Gorilla WebSocket · PostgreSQL (pgx) · Redis (go-redis/v9) ·
JWT (golang-jwt/v5) · zap · Docker Compose.

---

## 2. C4 Level 1 — System Context

Who talks to EchoStream, and what does EchoStream talk to?

```mermaid
graph TB
    subgraph Clients
        web["Web / Mobile Clients"]
        svc["Internal Services"]
        ops["Operators / On-call"]
    end

    subgraph EchoStream["EchoStream Platform"]
        es["Chat Backend<br/>(REST + WebSocket)"]
    end

    subgraph State["Stateful Systems"]
        pg[("PostgreSQL")]
        redis[("Redis")]
    end

    web -->|"REST / JSON (:8081)"| es
    web -->|"WebSocket (/v1/ws)"| es
    svc -->|"REST / JSON"| es
    ops -->|"/v1/health"| es

    es -->|"persist tenants, channels,<br/>messages, memberships"| pg
    es -->|"pub/sub fan-out,<br/>presence, rate limit"| redis
```

**Design note:** REST is the request/response front door (auth, channel CRUD, message history).
The WebSocket endpoint is the *live* channel — clients open one connection and subscribe to the
channels they care about. Both transports share the same persistence and business logic.

---

## 3. C4 Level 2 — Container View

The API server and the WebSocket hub run **in the same Go binary today** (the hub is a goroutine),
but the design keeps them decoupled through Redis so they can be split later.

```mermaid
graph TB
    client["Client"]

    subgraph Server["EchoStream Server (single Go binary)"]
        http["HTTP Server :8081<br/>Gin router + middleware"]
        ws["WebSocket Endpoint<br/>/v1/ws"]
        hub["Hub<br/>(single-owner event loop)"]
        bridge["Redis PubSub Bridge<br/>(Listen goroutine)"]
    end

    subgraph State["Stateful Systems"]
        pg[("PostgreSQL")]
        redis[("Redis")]
    end

    client -->|"REST"| http
    client <-->|"WebSocket frames"| ws

    http -->|"auth, rate limit"| redis
    http -->|"INSERT / SELECT<br/>(source of truth)"| pg
    http -->|"PUBLISH ch:&lt;id&gt;"| redis

    ws <--> hub
    hub -->|"SUBSCRIBE / UNSUBSCRIBE<br/>on first/last local sub"| bridge
    bridge -->|"SUBSCRIBE ch:*"| redis
    redis -->|"message payloads"| bridge
    bridge -->|"Broadcast(channelID, data)"| hub
    hub -->|"presence:&lt;userID&gt;"| redis
```

**Key insight (Redis as the horizontal-scaling backbone):** the HTTP handler that receives a message
does **not** need to hold the recipients' WebSocket connections. It writes to Postgres, then
`PUBLISH`es to `ch:<channelID>`. Every server instance that has at least one local subscriber to that
channel has an active Redis `SUBSCRIBE`, receives the payload, and fans it out to its local sockets.
This is the classic **pub/sub fan-out** pattern that lets any instance accept a write and every
instance deliver it.

---

## 4. C4 Level 3 — Internal Components

The Go packages and how they depend on each other. Arrows point in the direction of the dependency.

```mermaid
graph TD
    subgraph api["internal/api — HTTP + WS handlers (thin adapters)"]
        authH["auth.go"]
        chanH["channel.go"]
        memH["membership.go"]
        msgH["message.go"]
        userH["user.go"]
        presH["presence.go"]
        wsH["ws.go"]
    end

    subgraph mw["internal/middleware"]
        authMw["AuthMiddleware<br/>(JWT → context)"]
        rl["RateLimiter<br/>(Redis fixed window)"]
    end

    subgraph svc["internal/service — business rules"]
        msgSvc["MessageService<br/>(membership + limits + publish)"]
    end

    subgraph repo["internal/repository"]
        iface["interfaces.go<br/>(contracts)"]
        pgimpl["postgres/*<br/>(pgx implementations)"]
    end

    subgraph rt["Real-time"]
        hub["websocket/Hub"]
        client["websocket/Client"]
        pubsub["redis/PubSub"]
        tracker["presence/Tracker"]
    end

    msgH --> msgSvc
    chanH --> iface
    memH --> iface
    userH --> iface
    authH --> iface
    presH --> tracker
    presH --> iface

    msgSvc --> iface
    msgSvc -->|"publish OutboundEvent"| pubsub
    iface -.implemented by.-> pgimpl
    pgimpl --> pg[("PostgreSQL")]

    wsH --> hub
    hub --> client
    hub --> tracker
    hub <-->|"callbacks"| pubsub
    pubsub --> redis[("Redis")]
    rl --> redis
    tracker --> redis
    authMw -.protects.-> api
```

**The golden rule of the layering:** business logic lives in **services**, never in handlers.
`MessageHandler` parses the request, calls `MessageService.Send()`, and formats the response. The
service is where membership is checked, body length is validated, the row is persisted, and the
real-time event is published. Repositories are dumb data access behind interfaces — which makes the
services unit-testable with mocks.

```
HTTP Handler  →  Service  →  Repository Interface  →  Postgres impl
 (parse/format)  (rules)      (contract)              (SQL)
```

---

## 5. Data Model (ERD)

```mermaid
erDiagram
    TENANTS ||--o{ USERS : "owns"
    TENANTS ||--o{ CHANNELS : "owns"
    USERS ||--o{ CHANNEL_MEMBERS : "joins"
    CHANNELS ||--o{ CHANNEL_MEMBERS : "has"
    CHANNELS ||--o{ MESSAGES : "contains"
    USERS ||--o{ MESSAGES : "sends"

    TENANTS {
        uuid id PK
        text name
        timestamptz created_at
    }
    USERS {
        uuid id PK
        uuid tenant_id FK
        text email UK
        text display_name
        text password_hash
        timestamptz created_at
    }
    CHANNELS {
        uuid id PK
        uuid tenant_id FK
        text name
        boolean is_private
        timestamptz created_at
    }
    CHANNEL_MEMBERS {
        uuid channel_id PK,FK
        uuid user_id PK,FK
        text role
    }
    MESSAGES {
        bigserial id PK
        uuid tenant_id FK
        uuid channel_id FK
        uuid sender_id FK
        text body
        timestamptz created_at
    }
```

**Modeling notes & tradeoffs:**

- **`tenant_id` is the top-level isolation boundary.** It is denormalized onto `messages` (migration
  `003`) so message queries can filter by tenant without joining through `channels` — a deliberate
  read-performance-over-normalization tradeoff on the hottest table.
- **`messages.id` is `bigserial` (int64), not UUID.** Monotonic integers are cheaper to index and
  give a natural total order for cursor pagination (`WHERE id < before ORDER BY id DESC`). UUIDs would
  force ordering by `created_at`, which is not guaranteed unique.
- **`channel_members` is a composite-key join table.** `PRIMARY KEY (channel_id, user_id)` prevents
  duplicate memberships for free and doubles as the lookup index for the membership check on every
  send.
- **`is_private` on channels** gates access: public channels are joinable by any tenant member;
  private channels require an invite (enforced in the service/handler layer, not the schema).

---

## 6. Layered Request Lifecycle — Send a Message

`POST /v1/channels/:id/messages` — the canonical write path that touches every layer.

```mermaid
sequenceDiagram
    autonumber
    participant C as Client
    participant MW as Auth + RateLimit
    participant H as MessageHandler
    participant S as MessageService
    participant DB as Postgres
    participant R as Redis
    participant Hubs as All Server Hubs
    participant Subs as Subscribed Clients

    C->>MW: POST /v1/channels/:id/messages (JWT)
    MW->>MW: verify JWT → inject (user_id, tenant_id)
    MW->>MW: INCR rl:<user>:<bucket> (fail-open)
    MW->>H: forward request
    H->>S: Send(tenant, channel, sender, body)
    S->>S: validate body (non-empty, ≤ 4000 bytes)
    S->>DB: IsMember(channel, sender)?
    DB-->>S: true
    S->>DB: INSERT message → id, created_at
    DB-->>S: persisted message
    S->>R: PUBLISH ch:<channelID> {OutboundEvent}
    Note over S,R: publish is best-effort;<br/>message is already durable
    S-->>H: message
    H-->>C: 201 Created (message JSON)

    R-->>Hubs: deliver payload to every SUBSCRIBEd instance
    Hubs->>Subs: WebSocket frame {type:"message", ...}
```

**The critical ordering guarantee:** persist **then** publish. The database write is the commit
point. If the Redis `PUBLISH` fails, the message is still saved and the sender still gets a `201`;
the failure is logged, and the message will appear on the next history fetch (`GET .../messages`).
A Redis publish failure does not roll back the saved row. It can still leave a gap in live delivery,
which the client must recover through history reads.

---

## 7. Real-Time Delivery — WebSockets + Redis Fan-out

A single WebSocket connection multiplexes many channel subscriptions. The client speaks a small JSON
protocol over the socket:

| Direction | Type | Payload |
|---|---|---|
| client → server | `subscribe` | `{ "type": "subscribe", "channel_id": "<uuid>" }` |
| client → server | `unsubscribe` | `{ "type": "unsubscribe", "channel_id": "<uuid>" }` |
| client → server | `typing` | `{ "type": "typing", "channel_id": "<uuid>" }` |
| server → client | `subscribed` / `unsubscribed` | ack of the above |
| server → client | `message` | `{ "type": "message", "channel_id": "...", "message": {...} }` |
| server → client | `typing` | broadcast to *other* subscribers |
| server → client | `presence_change` | `{ "type": "presence_change", "user_id": "...", "status": "online\|offline" }` |
| server → client | `error` | `{ "type": "error", "error": "..." }` |

**Why Redis pub/sub and not in-memory only?** With a single instance you could fan out messages
directly from the hub. The moment you run two instances behind a load balancer, a sender on instance
A and a listener on instance B live in different process memory. Redis pub/sub is the shared bus that
reconnects them:

```mermaid
graph LR
    subgraph A["Instance A"]
        hA["Hub A"]
    end
    subgraph B["Instance B"]
        hB["Hub B"]
    end
    prod["Sender<br/>(HTTP on A)"] -->|PUBLISH ch:X| redis[("Redis")]
    redis -->|ch:X| hA
    redis -->|ch:X| hB
    hA --> cA["local subs of X on A"]
    hB --> cB["local subs of X on B"]
```

**Subscription lifecycle optimization:** an instance only holds a Redis `SUBSCRIBE` for channels it
currently has local subscribers for. The hub fires `onChannelActive` when the *first* local client
subscribes to a channel (→ `PubSub.Subscribe`) and `onChannelInactive` when the *last* one leaves
(→ `PubSub.Unsubscribe`). This keeps each instance's Redis subscription set proportional to its
actual live interest, not the global channel count.

---

## 8. The Hub Event Loop

The `Hub` (`internal/websocket/hub.go`) uses a **single-owner event loop**. Its mutable state —
which clients are in which channels, connection
counts per user, keepalive cancels — is owned by a **single goroutine** (`Hub.Run()`). Every mutation
arrives as a message on a channel; nothing is touched from the outside.

```mermaid
stateDiagram-v2
    [*] --> Running: go hub.Run()
    Running --> Running: register (new conn)
    Running --> Running: unregister (conn closed)
    Running --> Running: subscribe / unsubscribe
    Running --> Running: broadcast (from Redis)
    Running --> Running: typing
    Running --> [*]: shutdown
```

**Why no mutexes?** The hub multiplexes over Go channels in a `select`:

```go
for {
    select {
    case c := <-h.register:      // add connection, bump userConns, start keepalive
    case c := <-h.unregister:    // remove from all channels, stop keepalive, maybe offline
    case s := <-h.subscribeCh:   // add to channel; if first → onChannelActive
    case s := <-h.unsubscribeCh: // remove; if last → onChannelInactive
    case m := <-h.broadcastCh:   // fan out payload to a channel's local clients
    case e := <-h.typingCh:      // fan out typing to everyone *except* the sender
    case <-h.shutdown:           // exit
    }
}
```

`Run()` serializes map mutations. This avoids placing a mutex around each map operation, but
it is not a nonblocking algorithm: sends on the event channels can wait, and Redis subscription
and presence callbacks run in the Hub goroutine. A slow callback can delay other events.

**Per-connection plumbing** (`internal/websocket/client.go`): each `Client` has a buffered `send`
channel and a write pump goroutine. `Client.Send(data)` is a non-blocking enqueue — if a slow client
fills its buffer, the hub drops rather than blocking the whole event loop (backpressure isolation).

---

## 9. Presence & Typing

**Presence** (`internal/presence/tracker.go` + Redis):

```mermaid
sequenceDiagram
    autonumber
    participant C as Client
    participant H as Hub
    participant P as Presence Tracker
    participant R as Redis

    C->>H: WebSocket connect (register)
    H->>H: userConns[user]++
    H->>P: SetOnline(user)
    P->>R: SET presence:<user> "online"
    loop every 30s
        P->>R: refresh TTL (keepalive)
    end
    C->>H: disconnect (unregister)
    H->>H: userConns[user]--
    alt last connection for user on this instance
        H->>P: SetOffline(user)
        P->>R: DEL presence:<user>
    end
```

The Hub tracks `userConns map[uuid.UUID]int` within one process. Closing one of several local
connections leaves the presence key intact. However, closing the last local connection deletes
the shared Redis key even if another instance still has that user's connection. Its keepalive
uses `EXPIRE`, which does not recreate a deleted key. Cross-instance presence therefore needs a
different representation, such as per-connection leases, before it can be treated as accurate.

**Typing indicators** are intentionally *ephemeral and un-persisted*. A `typing` frame is fanned out
to the channel's other subscribers **on the same instance** and never touches Postgres or Redis
pub/sub. Presence-change events are also local. Clients connected to another instance do not
receive these events through the current implementation.

---

## 10. Cross-Cutting Concerns

### Authentication & Multi-Tenancy

- **JWT** (`internal/auth`) carries `(user_id, tenant_id, email)`. `middleware.AuthMiddleware`
  verifies the signature and injects claims into the Gin context.
- Handlers read identity via `middleware.GetUserID(c)` / `GetTenantID(c)` / `GetEmail(c)` — they
  never trust client-supplied tenant/user IDs from the body.
- Channel and message-history queries use `tenantID`; membership methods use channel/user IDs.
  These are different checks, and not every repository method accepts a tenant ID.
- Message sends and WebSocket subscriptions check membership. Message-history reads currently
  filter by tenant and channel but do not check membership, so private channels do not yet provide
  a complete read-access boundary. The WebSocket origin check also accepts all origins.

### Rate Limiting (`internal/middleware/ratelimit.go`)

- Fixed-window counter: key `rl:<userID>:<unix_time / window>`, atomic `INCR`, TTL = window.
- Current policy: **60 requests / minute / authenticated user**.
- **Fails open**: if Redis is unavailable, requests pass through. Availability is prioritized over
  strict enforcement for a chat product.
- *Tradeoff:* fixed windows allow a 2× burst at a boundary. A sliding-window log (Redis sorted set)
  would be smoother — noted in the roadmap.

### Graceful Shutdown (`cmd/server/main.go`)

On SIGINT/SIGTERM:
1. `http.Server.Shutdown()` stops accepting new connections; in-flight requests get 5s to finish.
2. Deferred teardown runs in dependency order: **pubsub → hub → redis → postgres → logger**.

### Observability

- Structured logging via **zap** (`internal/observ`), environment-aware (dev vs prod encoders).
- `GET /v1/health` liveness endpoint.
- *Gap:* no metrics/tracing yet — see roadmap.

---

## 11. Scaling & Capacity Planning

The message fan-out path can use multiple application instances with shared Redis and PostgreSQL.
Each instance still owns WebSocket state, typing events, and local presence counts. The following
are design considerations, not measured capacity results.

| Dimension | Bottleneck | Scaling lever |
|---|---|---|
| **HTTP throughput** | CPU on API instances | Add instances; they are stateless for REST. |
| **WebSocket fan-out** | Open FDs / memory per instance | Redis forwards message events between instances; test delivery gaps, backpressure, and reconnects before sizing. |
| **Message writes** | Postgres write IOPS | Connection pooling (pgxpool); later: partition `messages` by `tenant_id` or by time. |
| **Message history reads** | Index scans on hot channels | `(channel_id, id)` cursor index; later: read replicas / cache recent messages in Redis. |
| **Redis pub/sub** | Fan-out CPU on a single Redis | Redis Cluster / sharded pub/sub keyed by channel; or a dedicated broker (NATS/Kafka) for very high fan-out. |

**Illustrative sizing:** 10,000 users sending one message per minute would generate about
167 message writes per second. If each used the entire 60-request/minute allowance for writes,
the upper bound would instead be 10,000 writes per second, before fan-out. Neither workload has
been measured here. A channel with 50,000 subscribers would require up to 50,000 socket deliveries
per message; actual limits depend on payloads, connection counts, database resources, and failures.

**Routing:** REST sends and WebSocket recipients can be on different instances for message events.
That does not make every feature instance-independent: typing, presence-change events, and local
connection counts still need cross-instance coordination.

---

## 12. Failure Modes & Mitigations

| Failure | Impact | Mitigation |
|---|---|---|
| **Redis down** | No real-time fan-out, no rate limiting, presence stale. | Messages still persist and return `201`; rate limiter **fails open**; clients recover missed messages via history fetch. Real-time is explicitly best-effort. |
| **Postgres down** | Writes and history reads fail. | Requests return `5xx`; this is a hard dependency (source of truth). Mitigate with HA Postgres / failover. |
| **Slow WebSocket client** | Could stall delivery. | Per-client buffered `send` channel; non-blocking enqueue drops for a slow client rather than blocking the hub. |
| **Instance crash** | Its WebSocket connections drop. | Clients reconnect (to any instance), re-subscribe, and fetch missed history. No server-side session state is lost that matters. |
| **PUBLISH after persist fails** | Subscribers miss the live push. | Logged; message is durable and appears on next history load. |
| **Private-channel history request by a non-member in the same tenant** | History reads lack a membership check. | Known authorization gap; add and test the check before treating private channels as an access boundary. |
| **Last local connection closes while another instance still has the user** | The shared presence key is deleted prematurely. | Known limitation; per-connection presence leases are not implemented. |
| **Send immediately after channel creation** | Creator membership is inserted in a background goroutine after the response. | An immediate send or subscription can fail; channel creation and membership need a shared transaction. |
| **JWT secret rotation** | All active tokens invalidated. | Documented operational consequence; plan rotation windows. |
| **Rate-limit boundary burst** | Brief 2× over-limit. | Accepted tradeoff of fixed window; sliding window on roadmap. |

---

## 13. Key Design Decisions & Tradeoffs

| Decision | Why | Tradeoff / Cost |
|---|---|---|
| **Service layer holds all business rules** | Handlers stay thin and testable; rules live in one place. | More packages/indirection than putting logic in handlers. |
| **Repository interfaces + Postgres impl** | Swappable data layer; services unit-testable with mocks. | Boilerplate per entity. |
| **Persist-then-publish** | Durability first; real-time is additive. | A publish failure means a client relies on history fetch for that message. |
| **Redis pub/sub for fan-out** | Simple, horizontal scaling without sticky sessions. | Redis pub/sub is fire-and-forget (no replay); a subscriber offline during a publish misses the live event (recovered via history). |
| **Single-owner Hub** | One goroutine owns the maps, simplifying mutation order. | Events and Redis callbacks share a serialization point and can block one another. |
| **`bigserial` message IDs** | Cheap ordering + cursor pagination. | Sequential IDs leak volume; not globally unique across shards (fine for single primary). |
| **Denormalized `tenant_id` on messages** | Fast tenant-scoped reads without joins. | Slight write redundancy; must stay consistent with channel's tenant. |
| **Rate limiter fails open** | Availability > strictness for chat. | A Redis outage temporarily disables limiting. |

---

## 14. Roadmap — What to Build Next

The sections below are proposals. Before expanding the product, address private-channel history
authorization, transactional channel creation, cross-instance presence, and WebSocket origin
validation, then exercise them in integration tests.

### Phase 1 — Product depth (message semantics)
- **Message edits & deletes** (`PATCH`/`DELETE .../messages/:mid`) with `edited_at` / soft-delete
  columns, and `message_updated` / `message_deleted` WebSocket events. *Teaches:* mutation events,
  event versioning.
- **Reactions** (emoji) via a `message_reactions` table + `reaction_added/removed` events. *Teaches:*
  high-cardinality writes, aggregation.
- **Read receipts / unread counts** — per-user `last_read_message_id` per channel. *Teaches:*
  per-user cursor state, counting at scale.
- **Threaded replies** — self-referential `parent_message_id`. *Teaches:* hierarchical data modeling.

### Phase 2 — Reliability & correctness
- **Idempotency keys** on message send (Redis, Stripe-style) to dedupe client retries. *Teaches:*
  exactly-once semantics at the edge.
- **Reliable delivery / catch-up** — replace best-effort fan-out gaps with a per-connection
  "since message_id" resync on (re)subscribe. *Teaches:* at-least-once delivery, gap recovery.
- **Sliding-window rate limiter** (Redis sorted sets) replacing the fixed window. *Teaches:* the
  classic rate-limiter design-interview progression.
- **Outbox for cross-service events** if you add downstream consumers (search indexer, notifications).
  *Teaches:* transactional outbox and the difference between persistence and event publication.

### Phase 3 — Search & scale
- **Full-text message search** (Postgres `tsvector`, then Elasticsearch/OpenSearch). *Teaches:*
  search indexing, dual-write consistency.
- **Message partitioning** by time or tenant; **read replicas** for history. *Teaches:* sharding &
  read/write splitting.
- **Redis Cluster / sharded pub/sub** or a broker (NATS/Kafka) for fan-out at high subscriber counts.
  *Teaches:* messaging backbones beyond a single Redis.

### Phase 4 — Platform & operations
- **gRPC transport** for internal service-to-service calls (mirror Nimbus's dual-transport design).
  *Teaches:* Protobuf contracts, streaming.
- **Observability**: Prometheus metrics (connection counts, publish latency, delivery lag), OpenTelemetry
  tracing, `/metrics` endpoint. *Teaches:* production observability.
- **Automated DB migrations** in code (golang-migrate) instead of the init-script auto-apply.
- **Infrastructure as Code** (Terraform: ECS/Fargate or EKS, RDS, ElastiCache) + Dockerfile + CI.
  *Teaches:* deployment topology, the same IaC muscle Nimbus demonstrates.
- **Test coverage & load tests** — grow the table-driven unit tests, add integration tests
  (testcontainers) and a WebSocket load test (k6). *Teaches:* verification at scale.

The existing CI builds and tests the backend, builds and lints the frontend, and checks Compose
configuration and fresh schema initialization. Multi-instance behavior and load testing remain
separate work.

---

## 15. Design Principles Summary

1. **The database is the source of truth.** Everything real-time is a best-effort layer on top;
   nothing is acknowledged before it is durable.
2. **Business logic lives in services.** Handlers parse and format; repositories do SQL; services
   decide.
3. **Tenant filters and membership checks solve different problems.** Both are needed for private
   channel authorization; the current history-read gap is documented above.
4. **The Hub's mutable state has one owner.** Its event loop is a serialization point and can block.
5. **Redis decouples message producers from connections.** Message events can cross instances;
   typing and presence-change events currently remain local.
6. **Keep persistence separate from live delivery.** Redis outages can lose live events; saved
   message rows remain in PostgreSQL, and the rate limiter fails open.
7. **Shut down cleanly.** Drain HTTP, then tear down dependencies in order.
