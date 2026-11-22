# Cross-instance channel fan-out

In the recorded run, 20 different accounts held 20 simultaneous WebSocket connections to API B. Once every reader acknowledged its subscription to the same channel, k6 sent ten messages to API A, one every 100 ms. Every accepted message reached all 20 readers.

| Measurement | Recorded value |
| --- | ---: |
| Accepted sends | 10 of 10 |
| Unique reader/message pairs | 200 of 200 |
| Missing pairs / duplicate frames observed | 0 / 0 |
| Median POST start to WebSocket observation | 5 ms |
| p95 / p99 | 10 ms / 10 ms |
| Maximum | 10 ms |

Both Go processes, PostgreSQL 17.12, Redis 8.0.2 and k6 2.4.0 ran on one Linux amd64 host over loopback. Go 1.26.11 built the backend. The native test used an ephemeral Redis instance with persistence disabled and a freshly migrated database. One k6 virtual user coordinates all sockets and timestamps sends and receipts on the same clock. Each latency sample starts when k6 issues the sender's HTTP request and ends when k6 handles the first matching frame on one reader. The harness verifies durable IDs against the HTTP response and counts distinct reader/ID pairs. All subscribers were connected before measurement. The sample covers roughly one second of writes and excludes Internet links, TLS, client rendering and connection setup. It measures this workload on this machine. A sustained throughput limit would require a longer saturation test with independently sized nodes.

[Raw k6 summary](fanout-local.json) · [Test script](../../scripts/fanout.k6.js)

## Reproduce

Use a disposable database. Each run creates a workspace, users, a channel and messages. The Compose example runs PostgreSQL 15 and Redis 7. With ports 5173, 5432, 6379, 8081 and 8082 free, run these commands from the repo root.

```bash
docker compose -p echostream-bench up -d --build --wait
docker compose -p echostream-bench run -d --rm --no-deps --name echostream-bench-peer \
  -p 127.0.0.1:8082:8081 api
curl --fail --retry 15 --retry-all-errors --retry-delay 1 http://127.0.0.1:8082/v1/ready
RESULT=/tmp/echostream-fanout.json k6 run --quiet scripts/fanout.k6.js
docker stop echostream-bench-peer
docker compose -p echostream-bench down
```

The final command retains the benchmark volume. To use existing local test processes instead, set `WRITER` and `READER` to their distinct loopback HTTP origins. `READERS` (2–30) and `MESSAGES` (1–30) control the workload. k6 exits unsuccessfully if subscriptions, accepted sends, or unique deliveries fall short or if response validation fails. The summary reports duplicates observed through completion and always includes missing deliveries.

## Concurrent sends during reconnect

In a [recorded CI run](https://github.com/lalith-99/echostream/actions/runs/45012421471), two disposable Compose API instances shared PostgreSQL and Redis on one GitHub-hosted runner. The Node client used loopback HTTP, sent to both instances and connected three WebSocket readers through the frontend proxy. In each of 24 rounds the writers submitted one message apiece concurrently, then paused for one second. One reader disconnected for eight rounds.

| Measurement | Observed |
|---|---|
| Write window and final live drain | 24.2 s |
| Distinct committed messages | 48 |
| Distinct live IDs by reader | 48, 48, 32 |
| Offline IDs recovered via paginated history | 16 |
| Missing after final history comparison / duplicate live frames | 0 / 0 |
| Send initiation to first live observation, 128 reader-message pairs | p50 6.2 ms · p95 9.5 ms · p99 11.3 ms |

The elapsed time excludes signup, initial subscriptions and the final history scan. Latency uses one client clock. History requests used seven rows per page. The measurement covers paced traffic on a single runner. The receiver compares message IDs and bodies, since two concurrent HTTP requests may finish in either order.

The two uninterrupted readers could pass the original test even if the third reader stopped receiving live frames after it reconnected. A final history scan could conceal that regression. The [current workload](../../scripts/reconnect-load.mjs) checks the returning reader's live stream against the IDs sent while it was connected, and separately checks the IDs committed during its absence.

With the disposable Compose stack and peer running as above, run this command from the repository root using Node 22 or later.

```bash
node scripts/reconnect-load.mjs http://127.0.0.1:8082 http://127.0.0.1:5173
```
