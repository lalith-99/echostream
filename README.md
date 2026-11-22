# EchoStream

[![CI](https://github.com/lalith-99/echostream/actions/workflows/ci.yml/badge.svg)](https://github.com/lalith-99/echostream/actions/workflows/ci.yml)

EchoStream is a Go and React chat application. It grew out of a question about message delivery. If the server saves a message and the sender loses the HTTP response, can the sender safely try again? And if a recipient was offline, how do they find what they missed?

PostgreSQL stores messages and pending publications together. Redis carries live events between Go servers. A WebSocket gives the browser low-latency updates, and a cursor into PostgreSQL lets it recover after a disconnect. The repository includes the service, a working browser client, and tests that interrupt the delivery path.

## Run it

With Docker, Docker Compose v2 and Make installed:

```sh
git clone https://github.com/lalith-99/echostream.git
cd echostream
make up
```

Open **http://localhost:5173**, create a workspace and add a channel. To try chatting as two people, open a workspace invite in a separate browser session, sign up, then join the public channel or invite the second account to a private one. The web container proxies both REST and WebSocket traffic to the API.

```sh
curl --fail http://localhost:5173/v1/ready
make down
```

Compose builds the app, starts PostgreSQL and Redis and runs the schema migration before starting the API. Published ports bind to loopback. Stopping Compose retains the database volume. Local development credentials are for this isolated setup.

## Delivery and access

| If this happens | What EchoStream does |
| --- | --- |
| A send commits but the HTTP response is lost | A retry with the same UUID key and content returns the existing message. Reusing the key with different content returns 409. |
| Redis is unavailable after a send | The relay keeps the publication in PostgreSQL and retries with backoff. A running API that receives an authenticated request can still commit it. |
| The relay publishes, then crashes before completing its transaction | The event can be published twice. The client merges frames and history by the saved message ID. |
| The client disconnects or the gateway reconnects to Redis | The client fetches rows after its last confirmed history cursor, in ascending order. It also reconciles periodically while connected. |
| A user leaves a private channel | Membership checks cover history, sends and outbound channel frames. The gateway checks the recipient before writing each live frame. |
| The same user opens a second tab on another instance | Each connection owns a Redis lease. Closing one releases that lease. A crashed connection ages out after 90 seconds. |

Typing and immediate presence refresh hints are local to each gateway. Stored presence spans gateways. Workspace invite links are reusable until their seven-day expiry.

## Evidence

| Question | Reproducible evidence |
| --- | --- |
| Can concurrent retries insert two messages? | A PostgreSQL integration test races **12 sends with one key**, then checks for exactly one stored message and one outbox row. [Message integration tests](internal/repository/postgres/message_integration_test.go) |
| Does the recipient recover if Redis goes away? | The two-instance test stops Redis, writes through one API, retries through the other, checks history, restarts Redis, and observes the relay drain. [Recovery test](scripts/recovery.mjs) |
| Does a send cross the gateway boundary? | The smoke test sends on one API and receives the same durable ID over a WebSocket on the other. A separate test decodes OTLP and checks the trace and parent span IDs across those processes. [Smoke test](scripts/smoke.mjs) · [Trace test](scripts/tracecheck/trace_test.go) |
| What load was actually measured? | In a bounded local k6 run, **20 subscribed clients received all 10 messages** sent through a second API, for **200 distinct deliveries**. The report records latency, topology, versions and measurement limits. [Benchmark and raw results](docs/benchmarks/README.md) |

## Try the message API

After signing in, use the token returned by signup or login. Set `CHANNEL` to a channel you have joined and `KEY` to a UUID you will reuse if the response is lost.

```sh
curl -i "http://localhost:8081/v1/channels/$CHANNEL/messages" \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $KEY" \
  -H 'Content-Type: application/json' -d '{"content":"Did the deploy finish?"}'

curl --fail "http://localhost:8081/v1/channels/$CHANNEL/messages?after=0&limit=50" \
  -H "Authorization: Bearer $TOKEN"
```

The initial send returns 201. Repeating it returns 200 and the same message ID. A client that omits the optional key creates a new message on each POST.

## Develop and verify

The Go version is pinned in [go.mod](go.mod). Frontend development uses Node 22 and npm. For a host build with the stores in Compose:

```sh
docker compose up -d --wait postgres redis
docker compose run --rm --build migrate
make build
make run
# In a second terminal
cd web && npm ci && npm run dev
```

`make test` runs Go tests under the race detector. Set `TEST_DATABASE_URL` and `TEST_REDIS_URL` to disposable, migrated stores for integration tests. Set `ECHOSTREAM_TEST_REDIS_URL` to the same Redis URL for the presence and bridge tests. `TEST_SERVER_BIN` selects the compiled server for the two-process trace test. The test suite creates data. The [CI workflow](.github/workflows/ci.yml) also tests the web build, a legacy database upgrade, the nginx proxy and Redis recovery.

[Redis bridge allocation profile](docs/benchmarks/bridge.md) · [Trace a message in Jaeger](docs/tracing.md) · [Architecture and protocol](docs/ARCHITECTURE.md) · [Deployment and operations](docs/OPERATIONS.md) · [Frontend](web/README.md) · [Load test](docs/benchmarks/README.md)

## License

MIT
