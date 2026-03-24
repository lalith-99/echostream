# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Multi-tenant chat backend (Slack-style API) in Go with real-time WebSocket support. Postgres for persistence, Redis for pub/sub and rate limiting.

## Commands

```bash
# Start dependencies (Postgres + Redis)
docker compose up -d

# Run server (defaults to :8081)
make run
# or: go run ./cmd/server

# Build
make build    # → bin/server

# Tests
make test     # all tests
go test ./... # same
go test ./internal/api -v -run TestMessageHandler_Create  # single test
```

## Architecture

### Service Layer Pattern

```
HTTP Handlers (internal/api/)
    ↓
Services (internal/service/)      ← business rules enforced here
    ↓
Repository Interfaces (internal/repository/interfaces.go)
    ↓
Postgres Implementations (internal/repository/postgres/)
```

**Key point**: Business logic lives in services, not handlers. Handlers are thin adapters that parse requests and return responses. Example: `MessageHandler` calls `MessageService.Send()`, which validates rules (membership check, length limits) before calling `MessageRepository.Create()`.

### Multi-Tenancy

Every entity belongs to a `tenant_id`. JWT tokens contain `(user_id, tenant_id, email)` extracted by `middleware.AuthMiddleware()` and injected into Gin context. Retrieve with:
- `middleware.GetUserID(c)`
- `middleware.GetTenantID(c)`
- `middleware.GetEmail(c)`

All repository methods accept `tenantID uuid.UUID` for query scoping.

### Real-Time WebSocket Architecture

Horizontal scaling via Redis pub/sub:

```
POST /v1/channels/:id/messages
    ↓
MessageService.Send() → Postgres (persist) + Redis PUBLISH
    ↓                                            ↓
    ↓                                    All server instances
    ↓                                            ↓
    ↓                                    PubSub.Listen() (redis/pubsub.go)
    ↓                                            ↓
    ↓                                    Hub.Broadcast() (websocket/hub.go)
    ↓                                            ↓
 HTTP 200 ←                              Local WebSocket clients
```

**Hub** (`internal/websocket/hub.go`):
- Lock-free event loop managing all client state
- Maintains `channels map[uuid.UUID]map[*Client]struct{}` (channel → subscribers)
- Channel subscriptions trigger Redis pub/sub subscribe/unsubscribe via callbacks

**PubSub** (`internal/redis/pubsub.go`):
- Bridges Redis pub/sub with local Hub
- Topic format: `ch:<channelID>`
- When first local client subscribes to a channel → `Subscribe("ch:<uuid>")`
- When last local client unsubscribes → `Unsubscribe("ch:<uuid>")`

**Message flow**: Message persists to Postgres first (source of truth), then publishes to Redis (best-effort). If Redis publish fails, message is still saved and logged as warning.

**WebSocket protocol** (see `internal/websocket/message.go`):
- Client sends: `{"type": "subscribe", "channel_id": "<uuid>"}`
- Server sends: `{"type": "message", "channel_id": "<uuid>", "message": {...}}`
- Also: `typing`, `unsubscribe`, `subscribed`, `unsubscribed`, `error`

### Presence Tracking

`internal/presence/tracker.go` + Redis:
- Each WebSocket connection starts a `KeepAlive()` goroutine (30s intervals)
- Redis key: `presence:<userID>` → `"online"` or `"offline"`
- User only goes offline when their **last** connection closes
- Hub tracks `userConns map[uuid.UUID]int` to count connections per user

### Rate Limiting

`internal/middleware/ratelimit.go` uses Redis counters:
- Key: `rl:<userID>:<bucket>` where `bucket = unix_time / window_seconds`
- Atomic `INCR` + TTL for auto-cleanup
- **Fails open** if Redis unavailable (prioritizes availability)
- Current limit: 60 req/min per authenticated user

### Graceful Shutdown

`cmd/server/main.go` handles SIGINT/SIGTERM:
1. `http.Server.Shutdown()` stops accepting new connections
2. In-flight requests get 5 seconds to complete
3. Deferred cleanup chain: pubsub → hub → redis → postgres → logger

### Data Model Notes

- **Message** uses `bigserial` (int64) IDs, not UUID, for efficient ordering
- **Cursor pagination**: `ListByChannel(before int64, limit int)` where `before` is message ID
- **Tenant**: Top-level isolation boundary
- **Channel**: Can be public or private; access is enforced via membership checks in services

## Key Configuration

See `internal/config/config.go` for env vars. Defaults work for local Docker Compose setup. Important ones:
- `DATABASE_URL` - Postgres connection string
- `REDIS_URL` - Redis connection string
- `JWT_SECRET` - Changing this invalidates all active sessions

## Database Migrations

Located in `migrations/*.sql`. Docker auto-runs `.up.sql` files on first start via `scripts/init-db.sh`. Manually apply with `psql` if needed, but typically handled automatically.

## Testing

Existing tests use table-driven patterns. Mock repositories by implementing interfaces from `internal/repository/interfaces.go`. See:
- `internal/middleware/auth_test.go` - middleware tests
- `internal/api/message_test.go` - handler tests with mocks
- `internal/websocket/hub_test.go` - Hub event loop tests

## Important Architectural Constraints

1. **Never put business logic in handlers** - handlers parse/validate input, call services, format output
2. **Services coordinate between repos** - e.g., `MessageService.Send()` checks channel membership before saving via `MessageRepository`
3. **WebSocket state lives in Hub only** - no external state mutation, all changes go through Hub's channels
4. **Redis is best-effort** - code must handle Redis failures gracefully (rate limiter fails open, message broadcast logs and continues)
5. **Tenant isolation at query level** - every query includes `tenant_id` filter, even if data model enforces it via foreign keys