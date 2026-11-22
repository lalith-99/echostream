# Deploying and operating EchoStream

## Release

The default Compose stack is a runnable single-host development deployment. The Go image contains two unprivileged executables, `/app/server` and `/app/migrate`. The web image serves the static React build and proxies both HTTP and WebSocket upgrades. The supplied Fly configuration is a backend deployment template. The checked-in evidence covers local execution and CI. Public TLS, managed database failover and sustained production traffic depend on the chosen host.

1. Provision a dedicated PostgreSQL database with the `uuid-ossp` extension available and a private Redis instance. Redis pub/sub spans logical Redis databases, so give separate environments separate instances. Retain PostgreSQL backups. Provide Redis persistence for invitations that must survive a restart.
2. Build and identify the image revision. Configure credentials in the host's secret manager, terminate HTTPS at the ingress, and pass WebSocket upgrades through. The API must be reachable only through the intended ingress in a public deployment.
3. Run `/app/migrate` once per release with the new image and explicit `DATABASE_URL`. Migrations and their checksums commit in a transaction under a database advisory lock. The runner can adopt the original three unversioned migrations on an existing volume. A checksum disagreement stops the release.
4. Start API instances with shared storage and the **same JWT secret**. Use `GET /v1/ready` to admit traffic and `GET /v1/health` as a process liveness probe. Set the proxy read timeout above the 45-second WebSocket ping interval.
5. Sign in, send through one instance, subscribe through another, and check that both the received ID and stored history match. The [two-instance smoke](../scripts/smoke.mjs) performs this sequence against a dedicated test workspace.
6. Roll back by redeploying a previously tested, schema-compatible image. Run additive migrations before deploying code that needs them. Coordinate an incompatible change or a database restore as a separate release. The migration runner leaves existing data in place on failure.

## Settings

| Setting | Meaning |
| --- | --- |
| ENV | Set explicitly to production on a public host. Development is the local default. |
| DATABASE_URL, REDIS_URL | Required explicit connections in production. Use the provider's authenticated TLS endpoints, certificate settings and private networking. |
| JWT_SECRET | Supply a random signing secret of 32 bytes or more. Keep it consistent across replicas. Rotation invalidates previously issued tokens for new requests. Existing sockets expire at their JWT deadline. Restart gateways if earlier revocation is required. |
| FRONTEND_BASE_URL, CORS_ALLOWED_ORIGINS | Explicit HTTPS browser origin and comma-separated allowed HTTPS origins in production. Do not include paths. |
| PORT, LOG_LEVEL | Public listener port (8081 by default) and zap log level (info by default). |
| TRUSTED_PROXY_CIDRS | Optional comma-separated IPs/CIDRs of trusted ingress proxies. Leave unset for direct traffic. This affects the source IP used for public auth limits. The ingress must overwrite inbound forwarding headers. |
| ADMIN_PORT | Optional loopback-only management listener in each API container or host. Enables Prometheus text metrics and Go pprof. Keep profiling access private. |
| OTEL_EXPORTER_OTLP_ENDPOINT | Optional OTLP/HTTP collector base URL. A traces-specific endpoint is also supported. Configure sampling according to traffic and collector capacity. See the [local Jaeger walkthrough](tracing.md). |

Authenticated requests use a shared 60-request/minute limit per user. Signup and login share a 30-request/minute limit per client IP. The authenticated limiter fails open during a Redis outage to permit durable reads and writes. Public authentication fails closed. If the ingress always appears as one client, configure its exact trusted proxy ranges or all users behind it will share the auth budget.

## Metrics and recovery

With `ADMIN_PORT=9090` on a host-run API, use `curl --fail http://127.0.0.1:9090/metrics`. For a container, access that loopback address from the container or its network namespace. Metrics include requests and cumulative duration by route template/status, active WebSockets, pending publications and oldest pending age. Paths, tokens, user IDs and channel names are excluded from labels.

| Signal | First checks |
| --- | --- |
| Rising outbox age and pending count | Check Redis connectivity, relay logs and PostgreSQL locks. When Redis returns, watch the oldest age and row count fall. Preserve pending rows for replay. |
| Liveness 200, readiness 503 | Check Redis PING and the latest migration against the credentials used by this revision. Removing all unready replicas from a load balancer can interrupt all external traffic. |
| Live updates stop while history still advances | Compare the saved message ID against the receiver's history. Check socket expiry, gateway connection counts and resubscription. Use a trace to follow the send, outbox publish and receiving gateway fan-out. |
| High fan-out latency | Compare HTTP latency with outbox age. Each channel frame checks recipient membership in PostgreSQL. Check pool saturation before increasing process count. Capture a short CPU profile from the private pprof listener and compare against the same workload. |

The [recovery integration test](../scripts/recovery.mjs) deliberately stops Redis in its disposable Compose project. The [CI workflow](../.github/workflows/ci.yml) runs it against two API processes and then cleans up its test data.

## Restore drill

Create a logical backup from the intended source and restore it into a **new, empty database** created for the drill. Use credentials appropriate to the environment. Attach only test instances to the restored database, and use a separate Redis instance and JWT secret.

```sh
pg_dump --format=custom --file=echostream.dump "$SOURCE_DATABASE_URL"
pg_restore --exit-on-error --single-transaction --no-owner --no-acl \
  --dbname="$NEW_EMPTY_DRILL_DATABASE_URL" echostream.dump
DATABASE_URL="$NEW_EMPTY_DRILL_DATABASE_URL" ./bin/migrate
```

Verify the expected workspace, channels, membership and a known message ID before routing test traffic. An outbox row as of the backup can publish again when a worker starts. Redis presence is rebuilt as users reconnect. Workspace invite tokens live in Redis and require a Redis backup or reissue.
