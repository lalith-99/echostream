# Two-gateway WebSocket fan-out load

This workload tests the path from an HTTP send through PostgreSQL message/outbox commit, Redis publication, delivery-time membership checks and WebSocket receipt. All readers subscribe to one channel. Sixteen authenticated accounts each have eight sockets on each gateway, for 256 open connections. The first trial starts after every subscription is acknowledged.

The [Node driver](../../scripts/fanout-load.mjs) starts a burst of 16 POSTs every 1.5 seconds, alternating writers between gateways. Each POST carries a unique 512-byte ASCII body. Requests follow a monotonic schedule independent of acknowledgments, so a slow round can overlap the next. The single load driver records its own scheduling lag. Each trial runs 42 bursts, then waits for outstanding responses and drains the sockets for up to 10 seconds. Three consecutive trials reuse the same connections. Authentication, setup and the two-second final observation window are outside the reported trial duration.

Each connection records the ID and body of each first matching frame. At the end the driver compares these with the acknowledged message rows and checks for missing frames, duplicates, mismatches, request failures and socket closes. Latency runs from the individual POST's actual issue time to the driver observing the first matching WebSocket frame. It includes writer and reader event-loop scheduling. The reported delivery rate counts one verified message on one connection as one delivery. Half of the expected pairs cross gateways.

## Recorded trials

[Workflow run 45033172221](https://github.com/lalith-99/echostream/actions/runs/45033172221) used one GitHub-hosted Linux x64 runner with 4 available vCPUs and 15.6 GiB host memory (AMD EPYC 7763). Node 22.23.5 drove two Go gateways sharing PostgreSQL 15.20 and Redis 7.4.12. No CPU or memory quota was returned by the driver's cgroup-v2 probes.

| Trial | Send and drain | Committed writes | Verified deliveries | Deliveries/s | POST acknowledgment p95 | Live p50 / p95 / p99 |
|---|---:|---:|---:|---:|---:|---:|
| 1 | 62.01 s | 672 | 172,032 | 2,774 | 409.64 ms | 264.89 / 490.83 / 547.92 ms |
| 2 | 61.99 s | 672 | 172,032 | 2,775 | 349.40 ms | 259.98 / 479.55 / 503.97 ms |
| 3 | 62.00 s | 672 | 172,032 | 2,775 | 349.36 ms | 259.87 / 479.25 / 502.60 ms |

Across 2,016 commits the driver verified all 516,096 expected deliveries. It observed zero missing, duplicate or mismatched frames, HTTP failures or socket disconnects. The largest delay in starting a scheduled burst was 1.38 ms. Percentiles are per trial and the rate denominator includes the final live drain. [Raw JSON](fanout-load.json)

## Reproduce on a disposable stack

Use Docker Compose and Node 22 or newer. The commands below create a dedicated project and remove its benchmark data on cleanup. Choose a project name and peer container name unused on your machine. The workflow also supports a manual run in GitHub Actions.

```sh
docker compose -p echo-fanout up -d --build --wait api
docker compose -p echo-fanout run -d --no-deps --name echo-fanout-peer -p 127.0.0.1:8082:8081 api
curl --fail --retry 15 --retry-all-errors --retry-delay 1 http://127.0.0.1:8082/v1/ready
RESULT=fanout-load.json node scripts/fanout-load.mjs http://127.0.0.1:8081 http://127.0.0.1:8082
docker rm -f echo-fanout-peer
docker compose -p echo-fanout down --volumes
```

The run uses loopback networking, the default membership checks and HTTP rate limits, and Redis AOF with one-second fsync. PostgreSQL, Redis, both gateways and the load generator share a machine. These trials measure a fixed offered load for one hot channel. They do not estimate maximum capacity, performance across hosts, or long-lived session behavior.
