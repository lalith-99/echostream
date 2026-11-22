# WebSocket membership checks under fan-out

On each WebSocket message, the gateway verifies the recipient's current channel membership before writing the frame. In the original implementation each ready socket independently queried PostgreSQL. A single commit to a channel with 256 subscribed sockets could therefore trigger 256 membership queries, in addition to queries for presence and subscriptions.

The [batched implementation](../../internal/repository/postgres/membership_batch.go) allows up to four concurrent socket lookup queries per gateway. When calls overlap, a query can serve up to 64 currently waiting sockets and bind each distinct tenant/channel/user tuple once. A lone caller uses the original indexed query. New callers cannot reuse a previous SQL result; each request can expire on its own deadline. The [PostgreSQL integration test](../../internal/repository/postgres/channel_access_test.go) exercises revocation, tenant boundaries and cancellation.

## Matched workload

A [single GitHub Actions job](https://github.com/lalith-99/echostream/actions/runs/45034011449/job/137243619864) ran the original gateway and the candidate sequentially on the same host. Each variant started with fresh PostgreSQL and Redis volumes and two API gateways. The identical Node driver opened 256 authenticated WebSockets across 16 accounts on one hot channel, with eight sessions per account per gateway. Each trial scheduled 42 bursts of 16 concurrent sends at 1.5-second intervals. The 512-byte message bodies carried unique identifiers. Three trials reused the sockets. Every accepted HTTP row was matched by ID and body against the frame seen on each subscriber.

On this workload, membership SQL calls fell **93.5%** from **531,740 to 34,630**. Median per-trial p95 send-to-WebSocket latency fell **66.5%** from **236.68 ms to 79.32 ms**. Both revisions delivered all **516,096 expected reader/message pairs** from 2,016 accepted sends with zero missing, duplicate or mismatched frames and zero socket disconnects.

| Revision | Total membership SQL calls | Live p95, trials 1 / 2 / 3 (ms) | Live p99, trials 1 / 2 / 3 (ms) | Send and drain, trials 1 / 2 / 3 (s) |
|---|---:|---:|---:|---:|
| Original | 531,740 | 233.70 / 236.68 / 243.03 | 256.15 / 254.60 / 265.53 | 61.74 / 61.76 / 61.75 |
| Batched | 34,630 | 79.32 / 77.46 / 81.08 | 99.63 / 85.80 / 92.37 | 61.59 / 61.57 / 61.60 |

The Linux x64 runner exposed 4 CPUs and 15.6 GiB host memory (AMD EPYC 9V45 96-Core Processor). Node 22.23.5 drove both variants. Each trial verified 172,032 reader/message pairs. The baseline application was `1ed40e59`; Actions checked out PR #7 as test merge `6171b870`.

Membership counts come from `pg_stat_statements` matching the single and batched authorization SQL, reset for each new stack before account and subscription setup. They include presence and subscription traffic as well as the three send trials. The counts describe SQL calls at this workload, not rows examined or total database queries. Repeated user identities on a hot channel allow keys to coalesce.

Live latency is elapsed time on one driver clock from POST initiation to first observation on one reader. The p95 summaries are medians of three per-trial percentiles; they are not a pooled percentile of all frames. Send-and-drain time excludes setup and the final observation window. The benchmark applies a fixed offered rate. It does not measure maximum sustained throughput. Both the services and the load generator share the runner; the original always runs first.

## Reproduce

Use Linux with Bash, git history containing the baseline commit, Docker Compose, Node 22+ and free loopback ports 5432, 6379, 8081 and 8082. The script builds both revisions, creates and removes both disposable stacks, and leaves per-variant JSON and SQL call counts in the working directory.

```bash
bash scripts/membership-load.sh
```

[Driver](../../scripts/fanout-load.mjs) · [Comparison script](../../scripts/membership-load.sh) · [Original fan-out load test](fanout-load.md) · [Paired results](membership-load.json)
