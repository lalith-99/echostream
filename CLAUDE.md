# Working on EchoStream

The [README](README.md) contains the development commands and test setup. See [architecture](docs/ARCHITECTURE.md) for message ordering, access, presence, and recovery. See [operations](docs/OPERATIONS.md) for deployment configuration and troubleshooting.

The composition root is `cmd/server`. HTTP adapters live under `internal/api`, message business logic under `internal/service`, and SQL under `internal/repository/postgres`. The React client lives in `web`. Add forward schema changes under `migrations` and preserve checksums for migrations already deployed with the ledger.

Use disposable PostgreSQL and Redis instances for integration tests. The recovery suite restarts its designated Redis service.
