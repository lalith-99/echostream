# Reading chat history from a million-message database

The Go benchmark calls the same PostgreSQL repository method as the chat history API. It cycles through newest-first pages, older pages with an exclusive `before` cursor, and reconnect pages with an exclusive `after` cursor. The client checks each message's ID, position, channel, sender and body. Timing starts before the repository call acquires its connection and stops when it returns the page. The throughput calculation also includes client-side validation.

## Workload and controls

- A disposable PostgreSQL 15 database holds 1,000,000 synthetic historical messages interleaved among 16 private channels. The fixture contains 62,500 messages per channel and one member authorized in all channels. It preloads committed history directly and analyzes the tables.
- Each closed-loop trial uses 32 goroutines and a 25-connection pool. It warms up with 1,024 requests, then reads 65,536 pages of 20 messages. All versions use the same deterministic sequence of channel IDs, page directions and cursors.
- The script compiles the baseline from commit `1ed40e5` and the candidate with the identical load driver. Each link condition runs baseline, candidate, candidate, baseline, baseline, candidate on the same host and database. Go allocation samples cover the entire test process during the measured run. The trial percentiles cover repository latency individually.
- Direct trials connect over loopback. A second set uses Linux netem to add 2 ms of egress delay inside the disposable database container. Each tracked page includes the authorization check and repeatable-read transaction. These runs measure the read path independently of HTTP, WebSocket delivery, concurrent inserts and the production deployment network.

## Design decision

The original query selected the same channel UUID in every row and decoded it along with the sender UUID using a generic scanner. The decoder also repeatedly grew a slice before returning a page. The final path takes the channel ID from the already-constrained query argument, reads the sender UUID in PostgreSQL's binary representation, and fills a slice sized to the bounded page request. The original membership query, per-page MVCC snapshot and directional keyset ordering remain in place.

An earlier version combined authorization and pagination in one SQL statement using a lateral join. Its median direct-loopback p95 went from 9.21 to 11.87 ms, even though it avoided three round trips and did better over the delayed link. The sampled plan introduced a sort above the bounded page. We dropped the join. [Rejected experiment and plans](https://github.com/lalith-99/echostream/actions/runs/45033564712)

## Recorded comparison

[The measured run](https://github.com/lalith-99/echostream/actions/runs/45034202284) used Go 1.26.11 and PostgreSQL 15.20 on one GitHub-hosted Linux amd64 runner. The database reported 257 MB for messages and indexes. The 12 measured trials checked 786,432 pages and 15,728,640 returned records, with no errors or order/content mismatches. Each table cell is the median of the three trials for that condition and version. [All trial values](history-trials.csv)

| Link | Version | Pages/s | p50 ms | p95 ms | p99 ms | Allocated Go bytes/page | Go allocations/page |
|---|---|---:|---:|---:|---:|---:|---:|
| Loopback | Baseline | 7,677 | 4.06 | 6.22 | 7.27 | 19,721 | 329.4 |
| Loopback | Final | 8,012 | 3.90 | 5.89 | 6.95 | 10,690 | 202.3 |
| 2 ms DB egress | Baseline | 2,835 | 10.96 | 14.36 | 15.72 | 19,722 | 329.4 |
| 2 ms DB egress | Final | 2,851 | 10.51 | 14.87 | 16.00 | 10,692 | 202.3 |

Go allocated **46% fewer bytes per 20-message page** on loopback. Median throughput rose 4.4% there. With 2 ms added to database egress, the latency ranges overlap and the final median p95 was 0.51 ms higher. The benefit most consistent across trials and links was reduced allocation. These figures apply to this private-channel read fixture.

## Reproduce

Use a full clone with the baseline commit available, Docker, Go, and psql. The script builds both revisions, provisions a fresh loopback-only PostgreSQL container, seeds it, runs the comparison and removes the container. The optional delayed condition also requires Linux, `tc`, `nsenter` and sudo permission to change packet timing in that container's network namespace.

```sh
bash scripts/history/run.sh
# Include the second network condition used by the CI experiment
HISTORY_DELAYED_LINK=1 bash scripts/history/run.sh
```

[Load driver](../../internal/repository/postgres/history_load_test.go) · [Fixture](../../scripts/history/fixture.sql) · [Reader implementation](../../internal/repository/postgres/message.go) · [CI workflow](../../.github/workflows/history-benchmark.yml)
