#!/usr/bin/env bash
set -euo pipefail

# The baseline is the last main revision that reads in four DB commands.
base=1ed40e5995678a30e003593e61e993399085fb8a
root=$(git rev-parse --show-toplevel)
cd "$root"
tmp=$(mktemp -d)
container=
cleanup() {
  if [[ -n "$container" ]]; then docker rm -f "$container" >/dev/null; fi
  git worktree remove --force "$tmp/base" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

git worktree add --quiet --detach "$tmp/base" "$base"
cp internal/repository/postgres/history_load_test.go "$tmp/base/internal/repository/postgres/"
(cd "$tmp/base" && go test -c -o "$tmp/baseline" ./internal/repository/postgres)
go test -c -o "$tmp/candidate" ./internal/repository/postgres
go build -o "$tmp/migrate" ./cmd/migrate
container=$(docker run -d --rm -e POSTGRES_PASSWORD=local-test -p 127.0.0.1::5432 postgres:15-alpine -c shared_preload_libraries=pg_stat_statements)
port=$(docker port "$container" 5432/tcp | cut -d: -f2)
export HISTORY_LOAD_URL="postgres://postgres:local-test@127.0.0.1:$port/postgres?sslmode=disable"
for i in {1..60}; do
  if docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null; then break; fi
  sleep 1
done
DATABASE_URL="$HISTORY_LOAD_URL" "$tmp/migrate"
psql "$HISTORY_LOAD_URL" -X -q -v ON_ERROR_STOP=1 -f scripts/history/fixture.sql
psql "$HISTORY_LOAD_URL" -Xqc "CREATE EXTENSION pg_stat_statements"
go version
lscpu | awk '/^CPU\(s\):|^Model name:/'
git rev-parse HEAD
run_trials() {
  for version in baseline candidate candidate baseline baseline candidate; do
    echo "Trial: $version"
    psql "$HISTORY_LOAD_URL" -XqAtc 'SELECT pg_stat_statements_reset()' >/dev/null
    "$tmp/$version" -test.run '^TestHistoryReadLoad$' -test.v -test.timeout 3m
    psql "$HISTORY_LOAD_URL" -X -v ON_ERROR_STOP=1 -f scripts/history/plans.sql
  done
}
echo "Condition: direct loopback"
run_trials
if [[ "${HISTORY_DELAYED_LINK:-0}" == 1 ]]; then
  # Change packet timing only inside this disposable database container.
  # This adds 2 ms to outgoing packets, including replies to DB requests.
  pid=$(docker inspect -f '{{.State.Pid}}' "$container")
  sudo nsenter --target "$pid" --net tc qdisc add dev eth0 root netem delay 2ms
  echo "Condition: 2 ms database egress delay"
  run_trials
fi
