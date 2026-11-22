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
container=$(docker run -d --rm -e POSTGRES_PASSWORD=local-test -p 127.0.0.1::5432 postgres:15-alpine)
port=$(docker port "$container" 5432/tcp | cut -d: -f2)
export HISTORY_LOAD_URL="postgres://postgres:local-test@127.0.0.1:$port/postgres?sslmode=disable"
for i in {1..60}; do
  if docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null; then break; fi
  sleep 1
done
DATABASE_URL="$HISTORY_LOAD_URL" "$tmp/migrate"
psql "$HISTORY_LOAD_URL" -X -q -v ON_ERROR_STOP=1 -f scripts/history/fixture.sql
go version
lscpu | awk '/^CPU\(s\):|^Model name:/'
git rev-parse HEAD
for version in baseline candidate candidate baseline baseline candidate; do
  echo "Trial: $version"
  "$tmp/$version" -test.run '^TestHistoryReadLoad$' -test.v -test.timeout 3m
done
