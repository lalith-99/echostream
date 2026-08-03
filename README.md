<div align="center">

# 💬 EchoStream

**A full-stack, multi-tenant, real-time chat application.**

Go backend + React frontend — tenants, channels, memberships, and durable messages
over REST, with live delivery, presence, and typing indicators over WebSockets, fanned out across
horizontally-scaled instances by Redis pub/sub.

[![CI](https://github.com/lalith-99/echostream/actions/workflows/ci.yml/badge.svg)](https://github.com/lalith-99/echostream/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-pgx-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![Redis](https://img.shields.io/badge/Redis-pub%2Fsub%20%2B%20presence-DC382D?logo=redis&logoColor=white)](https://redis.io)

[Quick Start](#-quick-start) · [Architecture](docs/ARCHITECTURE.md) · [API](#-api-surface) · [Frontend](web/README.md)

</div>

---

## What is EchoStream?

EchoStream solves one problem well: **accept, durably persist, and deliver chat messages in real time
for thousands of independent tenants — while scaling horizontally with no single-instance bottleneck.**

Every message is written to PostgreSQL *before* the sender is acknowledged — the database is the
single source of truth. Real-time delivery is a best-effort layer on top: a new message is
`PUBLISH`ed to Redis and fanned out to every server instance that holds a WebSocket subscriber for
that channel. Any instance can accept a write; every instance can deliver it. No sticky sessions
required for correctness.

```mermaid
graph LR
    web["Web / Mobile Clients"] -->|REST :8081| srv
    web <-->|WebSocket /v1/ws| srv

    subgraph srv["EchoStream Server"]
        api["HTTP + WS<br/>(Gin)"] -->|INSERT / SELECT| db
        api -->|PUBLISH ch:id| redis
        hub["Hub<br/>(lock-free loop)"]
        bridge["Redis PubSub bridge"] --> hub
    end

    db[("PostgreSQL")]
    api -.->|auth · rate limit · presence| redis[("Redis")]
    redis -->|ch:id fan-out| bridge
    hub --> web
```

> 📐 **For system-design depth — C4 diagrams, sequence flows, the lock-free Hub state machine,
> scaling math, failure modes, and tradeoff tables — read the
> [Architecture Deep Dive](docs/ARCHITECTURE.md).**

---

## ✨ Features

| Capability | Detail |
|---|---|
| **Multi-tenancy** | Every entity is tenant-scoped. JWT carries `(user_id, tenant_id, email)`; every query filters by `tenant_id`. |
| **Durable messages** | Persisted to Postgres before the sender is acknowledged. The DB is the source of truth; real-time is additive. |
| **Real-time delivery** | WebSocket connections held by a lock-free **Hub**; new messages fanned out over **Redis pub/sub** across all instances. |
| **Horizontal scaling** | A write on any instance reaches subscribers on every instance — no sticky sessions needed for delivery correctness. |
| **Presence** | Redis-backed online/offline with a keepalive heartbeat; a user goes offline only when their **last** connection drops (multi-device safe). |
| **Typing indicators** | Ephemeral, un-persisted `typing` events fanned out to a channel's other subscribers. |
| **Channels & memberships** | Public/private channels, join/leave/invite, membership checks enforced in the service layer. |
| **Cursor pagination** | `bigserial` message IDs give cheap total ordering and `before`-cursor history fetches. |
| **Rate limiting** | Per-user fixed-window counter in Redis (60 req/min); **fails open** on Redis outage. |
| **Graceful shutdown** | SIGINT/SIGTERM drains in-flight requests, then tears down pubsub → hub → redis → postgres → logger. |

---

## 🏗️ Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.25 |
| HTTP | Gin router + middleware |
| Real-time | Gorilla WebSocket |
| Datastore | PostgreSQL (pgx / pgxpool) |
| Cache / coordination | Redis (go-redis/v9) — pub/sub, presence, rate limiting |
| Auth | JWT (golang-jwt/v5), bcrypt password hashing |
| Observability | Structured logging (zap) |
| Packaging | Docker Compose (Postgres + Redis) |
| **Frontend** | **React 19 + TypeScript (Vite), Tailwind CSS, TanStack Query, Zustand** |

---

## 🚀 Quick Start

### Prerequisites

- Go 1.25+
- Docker & Docker Compose

### Run locally

```bash
# 1. Start dependencies (Postgres + Redis). Migrations auto-apply on first start.
docker compose up -d

# 2. Run the server (defaults to :8081)
make run          # or: go run ./cmd/server
```

Config is read from environment variables with sensible local-dev defaults — see
[`internal/config/config.go`](internal/config/config.go).

### Build & test

```bash
make build        # → bin/server
make test         # go test ./...
```

### Smoke test

```bash
# Health
curl http://localhost:8081/v1/health          # → {"status":"ok"}

# Create an account (also creates the tenant), then log in for a JWT
curl -X POST http://localhost:8081/v1/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"tenant_name":"acme","email":"a@acme.com","display_name":"Ada","password":"s3cret!!"}'

curl -X POST http://localhost:8081/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"a@acme.com","password":"s3cret!!"}'
# → { "token": "<JWT>" }

# Use the token for authenticated routes
TOKEN=<JWT>
curl -X POST http://localhost:8081/v1/channels \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"general","is_private":false}'
```

For live delivery, open a WebSocket to `ws://localhost:8081/v1/ws` (with the JWT), send
`{"type":"subscribe","channel_id":"<uuid>"}`, and watch `message` / `typing` / `presence_change`
frames arrive.

---

## ⚙️ Configuration

All configuration is via environment variables (sensible defaults for local dev).

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8081` | HTTP/REST + WebSocket port. |
| `DATABASE_URL` | `postgres://echostream:echostream123@localhost:5432/echostream?sslmode=disable` | PostgreSQL connection string. |
| `REDIS_URL` | `redis://localhost:6379` | Redis (pub/sub, presence, rate limiting). |
| `ENV` | `development` | Runtime environment (affects log encoding). |
| `LOG_LEVEL` | `info` | Log verbosity. |
| `JWT_SECRET` | `dev-secret-do-not-use-in-prod` | HMAC secret. Changing it invalidates all active sessions. |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173,http://localhost:3000` | Comma-separated browser origins allowed to call the API. Use `*` for any origin (local dev only). |
| `FRONTEND_BASE_URL` | `http://localhost:5173` | Base URL of the web app; used to build workspace invite links. |

---

## 🔌 API Surface

**Public** (no auth):

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/health` | Liveness check. |
| `POST` | `/v1/auth/signup` | Create tenant + account. |
| `POST` | `/v1/auth/login` | Exchange credentials for a JWT. |
| `GET` | `/v1/ws` | WebSocket upgrade (JWT-authenticated). |

**Authenticated** (JWT required, rate-limited 60 req/min/user):

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/channels` | Create a channel. |
| `GET` | `/v1/channels` | List channels (paginated). |
| `GET` | `/v1/channels/:id` | Get a channel. |
| `POST` | `/v1/channels/:id/messages` | Send a message (persist + fan-out). |
| `GET` | `/v1/channels/:id/messages` | List messages (cursor pagination). |
| `POST` | `/v1/channels/:id/join` | Join a channel. |
| `POST` | `/v1/channels/:id/leave` | Leave a channel. |
| `POST` | `/v1/channels/:id/invite` | Invite a user to a channel. |
| `GET` | `/v1/channels/:id/members` | List channel members. |
| `GET` | `/v1/channels/:id/presence` | Online/offline status of channel members. |
| `GET` | `/v1/users/me` | Current user info. |

**WebSocket protocol** (`/v1/ws`):

| Direction | Type | Payload |
|---|---|---|
| → server | `subscribe` / `unsubscribe` | `{ "type": "...", "channel_id": "<uuid>" }` |
| → server | `typing` | `{ "type": "typing", "channel_id": "<uuid>" }` |
| ← client | `message` | `{ "type": "message", "channel_id": "...", "message": {...} }` |
| ← client | `subscribed` / `unsubscribed` | subscription ack |
| ← client | `typing` | broadcast to a channel's *other* subscribers |
| ← client | `presence_change` | `{ "type": "presence_change", "user_id": "...", "status": "online\|offline" }` |
| ← client | `error` | `{ "type": "error", "error": "..." }` |

---

## 🗂️ Project Structure

```
echostream/
├── cmd/server/           # Composition root — wires config, db, redis, hub, services, handlers
├── internal/
│   ├── api/              # HTTP + WebSocket handlers (thin adapters)
│   ├── service/          # Business rules (MessageService: membership + limits + publish)
│   ├── repository/       # Data access — interfaces.go + postgres/ implementations
│   ├── websocket/        # Lock-free Hub, per-connection Client, wire protocol
│   ├── redis/            # Redis client + pub/sub bridge to the Hub
│   ├── presence/         # Online/offline tracker (Redis + keepalive)
│   ├── middleware/       # JWT auth + Redis rate limiter
│   ├── auth/             # JWT issue/verify helpers
│   ├── models/           # Domain types
│   ├── config/           # Env-based configuration
│   ├── db/               # Postgres connection (pgxpool)
│   └── observ/           # Structured logging (zap)
├── migrations/           # SQL migrations (auto-applied on first Docker start)
├── scripts/              # DB init script
├── docker-compose.yml    # Local Postgres + Redis
└── docs/                 # ARCHITECTURE.md — the system-design deep dive
```

---

## 🧠 Architectural Constraints

1. **Never put business logic in handlers** — handlers parse/validate input, call services, format output.
2. **Services coordinate repositories** — e.g. `MessageService.Send()` checks membership before persisting.
3. **WebSocket state lives in the Hub only** — all mutations flow through its channels; no external state mutation, no locks.
4. **Redis is best-effort** — the rate limiter fails open, and message fan-out logs-and-continues on publish failure. Messages are never lost because the DB is the source of truth.
5. **Tenant isolation at the query level** — every query includes a `tenant_id` filter.

---

## 📚 Documentation

- **[Architecture Deep Dive](docs/ARCHITECTURE.md)** — C4 diagrams, the send-message lifecycle,
  Redis fan-out, the lock-free Hub, presence, scaling, failure modes, tradeoffs, and the build-out roadmap.

---

## License

MIT