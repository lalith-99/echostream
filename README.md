<div align="center">

# EchoStream

**A Go and React chat application with PostgreSQL message history and Redis pub/sub.**

REST APIs handle workspaces, channels, memberships, and messages. WebSockets deliver live
updates; Redis pub/sub carries message events between server instances. This is a personal
development project. Capacity and production readiness have not been established.

[![CI](https://github.com/lalith-99/echostream/actions/workflows/ci.yml/badge.svg)](https://github.com/lalith-99/echostream/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-pgx-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![Redis](https://img.shields.io/badge/Redis-pub%2Fsub%20%2B%20presence-DC382D?logo=redis&logoColor=white)](https://redis.io)

[Quick Start](#quick-start) · [Architecture](docs/ARCHITECTURE.md) · [API](#api-surface) · [Frontend](web/README.md)

</div>

---

## What is EchoStream?

EchoStream separates stored message history from live delivery. A send writes to PostgreSQL,
attempts to publish a live update through Redis, and then returns the saved message to the caller.

Every message is written to PostgreSQL *before* the sender is acknowledged — the database is the
single source of truth. Real-time delivery is a best-effort layer on top: a new message is
`PUBLISH`ed to Redis and fanned out to every server instance that holds a WebSocket subscriber for
that channel. This supports message fan-out between instances, but Redis pub/sub does not replay
missed events. Clients must fetch history to recover gaps.

```mermaid
graph LR
    web["Web / Mobile Clients"] -->|REST :8081| srv
    web <-->|WebSocket /v1/ws| srv

    subgraph srv["EchoStream Server"]
        api["HTTP + WS<br/>(Gin)"] -->|INSERT / SELECT| db
        api -->|PUBLISH ch:id| redis
        hub["Hub<br/>(single-owner event loop)"]
        bridge["Redis PubSub bridge"] --> hub
    end

    db[("PostgreSQL")]
    api -.->|auth · rate limit · presence| redis[("Redis")]
    redis -->|ch:id fan-out| bridge
    hub --> web
```

The [architecture notes](docs/ARCHITECTURE.md) explain the write path, Hub event loop,
message fan-out, and current limitations.

---

## Implemented behavior

| Capability | Detail |
|---|---|
| **Workspaces** | JWT carries `(user_id, tenant_id, email)`. Channel and message-history reads use tenant IDs; membership lookups use channel/user IDs. See the authorization limitation below. |
| **Durable messages** | Persisted to Postgres before the sender is acknowledged. The DB is the source of truth; real-time is additive. |
| **Real-time delivery** | A Hub goroutine owns local WebSocket state; Redis pub/sub forwards message events to subscribed instances on a best-effort basis. |
| **Presence** | Redis stores a 90-second presence key, refreshed every 30 seconds. Connection counts are local to each server instance. |
| **Typing indicators** | Ephemeral `typing` events reach the channel's other subscribers on the same server instance. |
| **Channels & memberships** | Public/private channel flags and join/leave/invite APIs. Sending a message and subscribing over WebSocket check membership. |
| **Cursor pagination** | `bigserial` message IDs give cheap total ordering and `before`-cursor history fetches. |
| **Rate limiting** | Per-user fixed-window counter in Redis (60 req/min); **fails open** on Redis outage. |
| **Graceful shutdown** | SIGINT/SIGTERM drains in-flight requests, then tears down pubsub → hub → redis → postgres → logger. |

---

## Tech stack

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

## Quick start

### Prerequisites

- Go 1.25+
- Docker with the Compose v2 plugin (`docker compose`) and support for `up --wait`
- Make, or use `go run ./cmd/server` directly
- Node.js 22 and npm for frontend development; Docker can build the optional frontend instead

### Run locally

```bash
git clone https://github.com/lalith-99/echostream.git
cd echostream

# 1. Start dependencies (Postgres + Redis). Migrations auto-apply on first start.
docker compose up -d --wait postgres redis

# 2. Run the server (defaults to :8081)
make run          # or: go run ./cmd/server
```

Config is read from environment variables with sensible local-dev defaults — see
[`internal/config/config.go`](internal/config/config.go).

Keep the backend running. From the repository root in a second terminal:

```bash
cd web
npm ci
npm run dev
```

Open `http://localhost:5173`, create an account, then create a channel. Use a workspace invite
to add a second account to the same workspace; a separate signup without an invite creates a
different workspace. The backend listens on `http://localhost:8081`.

Alternatively, from the repository root, build and serve the frontend in Docker:

```bash
docker compose --profile full up -d --build --wait
```

This profile starts the frontend and its PostgreSQL/Redis dependencies. **The Go backend still
runs separately with `make run`.** The frontend remains at `http://localhost:5173`, matching the
backend's default CORS allowlist and invite URLs. Do not run the Vite dev server and containerized
frontend on port 5173 at the same time.

Compose publishes PostgreSQL, Redis, and the optional frontend on loopback only. The Go server
itself listens on all interfaces; use the local defaults only for development. `docker compose down`
stops the containers and retains the database volume. Initialization scripts run only on a new
database volume; they do not upgrade an existing database.

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
TOKEN='paste-the-token-from-login-here'
curl -X POST http://localhost:8081/v1/channels \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"general","is_private":false}'
```

For live delivery, open a WebSocket to `ws://localhost:8081/v1/ws` (with the JWT), send
`{"type":"subscribe","channel_id":"<uuid>"}`, and watch `message` / `typing` / `presence_change`
frames arrive.

---

## Current limitations

- **Live delivery is best-effort.** A saved message can miss its Redis publish or a WebSocket
  delivery. There is no transactional outbox, delivery acknowledgment, or automatic replay cursor.
- **Presence is not coordinated across instances.** Closing the last local connection deletes
  a shared Redis presence key even if another instance still holds a connection for that user.
  Typing and `presence_change` events stay within the local Hub.
- **Private-channel reads need further authorization work.** Message history filters by tenant
  and channel but does not check membership. The private flag is not a complete read-access boundary.
- **Channel creation and creator membership are separate writes.** The API returns before its
  background membership insert completes, so an immediate send or subscribe can be rejected.
- **Deployment hardening is unfinished.** The WebSocket upgrade currently accepts all origins,
  the local JWT secret is a development default, and no load-test capacity is claimed.

These limits are documented so the implementation can be evaluated as it stands.

## Configuration

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

## API surface

**Public** (no auth):

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/health` | Liveness check. |
| `POST` | `/v1/auth/signup` | Create tenant + account. |
| `POST` | `/v1/auth/login` | Exchange credentials for a JWT. |
| `GET` | `/v1/ws` | WebSocket upgrade; separately validates a JWT in the `token` query parameter. |

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

## Project structure

```
echostream/
├── cmd/server/           # Composition root — wires config, db, redis, hub, services, handlers
├── internal/
│   ├── api/              # HTTP + WebSocket handlers (thin adapters)
│   ├── service/          # Business rules (MessageService: membership + limits + publish)
│   ├── repository/       # Data access — interfaces.go + postgres/ implementations
│   ├── websocket/        # Hub event loop, per-connection Client, wire protocol
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

## Design choices

1. **Never put business logic in handlers** — handlers parse/validate input, call services, format output.
2. **Services coordinate repositories** — e.g. `MessageService.Send()` checks membership before persisting.
3. **The Hub owns its state** — map mutations flow through one goroutine. Channel sends and Redis callbacks can still block; this is not a nonblocking algorithm.
4. **PostgreSQL and live delivery are separate** — the rate limiter fails open, and message fan-out logs-and-continues on publish failure. Saved history remains available through the REST API.
5. **Authorization needs checks at each boundary** — tenant filters and membership checks serve different purposes; the current private-history gap is listed above.

---

## Verification and documentation

GitHub Actions builds, vets, and tests the Go backend; installs, lints, and builds the frontend;
validates both Compose profiles; and checks that a fresh PostgreSQL volume receives the schema
migrations. These checks do not establish multi-instance delivery behavior or load capacity.

- **[Architecture notes](docs/ARCHITECTURE.md)** — diagrams, the send-message lifecycle,
  Redis fan-out, the Hub, presence, failure modes, and planned work.

---

## License

MIT
