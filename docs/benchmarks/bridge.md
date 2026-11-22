# Redis bridge allocation profile

The gateway receives one JSON event from Redis and hands its public fields to the local WebSocket hub. In the original implementation every event became a map of JSON values, including a copy of the message body. An allocation profile of both test cases attributed 29.2% of sampled bytes to copying raw JSON values and 13.8% to reflective map writes.

The optimized path validates an untraced JSON object and forwards its existing bytes. Events that could include private trace metadata use the full decoder, which removes the top-level field and preserves the public payload. Regression tests cover older envelopes, extra fields, escaped names, nested user content and malformed frames.

## Recorded comparison

Both runs used Go 1.26.11 on Linux/amd64, identical dependencies (including OpenTelemetry 1.45.0), and one logical CPU on an AMD EPYC 9V74. The baseline bridge came from commit `9d85d946ca1b867f7647f58051c2f711629f7bb3`. Each input ran five one-second trials. The fixture wraps a 444-byte chat body in a 683-byte public event. Adding a W3C trace context makes the input 755 bytes. The broadcaster counts bytes in memory.

| Redis input | Before, allocations/op | After, allocations/op | Before, bytes/op | After, bytes/op |
| --- | ---: | ---: | ---: | ---: |
| Public message | 22 | 6 | 1,976 | 592 |
| Message with trace context | 37 | 37 | 3,352 | 3,352 |

The public case allocated **70.0% fewer bytes per event**. Each of the five runs reported the same allocation counts. Redis network I/O, PostgreSQL authorization checks, socket writes and exporter I/O fall outside this benchmark. The [cross-instance fan-out test](README.md) exercises the running application.

## Reproduce the comparison

Run from the repository root with Git and a Go toolchain that supports automatic toolchain selection. The commands use a temporary worktree so both implementations run against identical dependencies.

```bash
repo=$(pwd)
run=$(mktemp -d)
git worktree add --detach "$run/src" 9d85d946ca1b867f7647f58051c2f711629f7bb3
cp go.mod go.sum "$run/src/"
cp internal/redis/bridge_benchmark_test.go "$run/src/internal/redis/"
(
  cd "$run/src"
  export GOTOOLCHAIN=go1.26.11
  measure() {
    go test ./internal/redis -run '^$' -bench '^BenchmarkBridgeDelivery$' \
      -benchmem -cpu=1 -benchtime=1s -count=5 \
      -memprofile="$run/$1.pprof" -o "$run/$1.test"
    go tool pprof -top -alloc_space "$run/$1.test" "$run/$1.pprof"
  }
  measure before
  cp "$repo/internal/redis/pubsub.go" internal/redis/
  measure after
)
git worktree remove --force "$run/src"
```

For a quick measurement with the current checkout and toolchain, run `go test ./internal/redis -run '^$' -bench '^BenchmarkBridgeDelivery$' -benchmem -cpu=1`.
