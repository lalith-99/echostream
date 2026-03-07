# EchoStream

Multi-tenant chat backend in Go. Think Slack, but just the API layer — channels, messages, memberships, auth.

## Stack

- **Go** + [Gin](https://github.com/gin-gonic/gin) for HTTP
- **Postgres** for persistence (pgx)
- **Redis** (WIP)
- **JWT** auth with tenant isolation
- **Docker Compose** for local Postgres + Redis

## Getting started

### Prerequisites

- Go 1.25+
- Docker & Docker Compose

### Run locally

```bash
# spin up Postgres and Redis
docker compose up -d

# run the server (defaults to :8081)
make run
```

The server reads config from env vars with sane defaults for local dev. See `internal/config/config.go` for the full list.

### Build

```bash
make build    # outputs bin/server
make test     # runs all tests
```

## API overview

Public routes (no auth):

| Method | Path              | Description       |
|--------|-------------------|--------------------|
| GET    | `/v1/health`      | Health check       |
| POST   | `/v1/auth/signup`  | Create account     |
| POST   | `/v1/auth/login`   | Get JWT token      |

Authenticated routes (JWT required):

| Method | Path                          | Description              |
|--------|-------------------------------|--------------------------|
| POST   | `/v1/channels`                | Create a channel         |
| GET    | `/v1/channels`                | List channels            |
| GET    | `/v1/channels/:id`            | Get a channel            |
| POST   | `/v1/channels/:id/messages`   | Send a message           |
| GET    | `/v1/channels/:id/messages`   | List messages            |
| POST   | `/v1/channels/:id/join`       | Join a channel           |
| POST   | `/v1/channels/:id/leave`      | Leave a channel          |
| GET    | `/v1/channels/:id/members`    | List channel members     |
| GET    | `/v1/users/me`                | Current user info        |

## Project layout

```
cmd/server/          entrypoint
internal/
  api/               HTTP handlers
  auth/              JWT helpers
  config/            env-based config
  db/                Postgres connection
  middleware/        auth middleware
  models/            domain types
  observ/            logging (zap)
  repository/        data access (interfaces + postgres impl)
  websocket/         (WIP)
migrations/          SQL migrations
```

## Config

All config comes from environment variables. Defaults are set for local dev.

| Var             | Default                                                                 |
|-----------------|-------------------------------------------------------------------------|
| `PORT`          | `8081`                                                                  |
| `DATABASE_URL`  | `postgres://echostream:echostream123@localhost:5432/echostream?sslmode=disable` |
| `REDIS_URL`     | `redis://localhost:6379`                                                |
| `ENV`           | `development`                                                           |
| `LOG_LEVEL`     | `info`                                                                  |
| `JWT_SECRET`    | `dev-secret-do-not-use-in-prod`                                         |

## License

MIT