# Cross-instance channel fan-out

In the recorded run, 20 different accounts held 20 simultaneous WebSocket connections to API B. Once every reader acknowledged its subscription to the same channel, k6 sent ten messages to API A, one every 100 ms. Every accepted message reached all 20 readers.

| Measurement | Recorded value |
| --- | ---: |
| Accepted sends | 10 of 10 |
| Unique reader/message pairs | 200 of 200 |
| Missing pairs / duplicate frames observed | 0 / 0 |
| Median POST start to WebSocket observation | 16.5 ms |
| p95 / p99 | 35 ms / 41.09 ms |
| Maximum | 55 ms |

Both Go processes, PostgreSQL 17.12, Redis 8.0.2 and k6 2.4.0 ran on one Linux amd64 host over loopback. Go 1.25.6 built the backend. Each latency sample starts when k6 issues the sender's HTTP request and ends when k6 handles the first matching frame on one reader. The harness verifies durable IDs against the HTTP response and counts distinct reader/ID pairs. All subscribers were connected before measurement. The sample covers roughly one second of writes and excludes Internet links, TLS, client rendering and connection setup. It measures this workload on this machine. A sustained throughput limit would require a longer saturation test with independently sized nodes.

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
