#!/usr/bin/env bash
# Two independent disposable Compose stacks, run sequentially on the same host.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
base=1ed40e5995678a30e003593e61e993399085fb8a
tmp=$(mktemp -d)
export COMPOSE_PROJECT_NAME="echoauthbench${RANDOM}"
cat > "$tmp/stats.yml" <<'YAML'
services:
  postgres:
    command: ["postgres", "-c", "shared_preload_libraries=pg_stat_statements"]
YAML
project_dir=
compose() {
  docker compose --project-directory "$project_dir" -f "$project_dir/docker-compose.yml" -f "$tmp/stats.yml" "$@"
}
cleanup_stack() {
  docker rm -f "$COMPOSE_PROJECT_NAME-peer" >/dev/null 2>&1 || true
  if [[ -n "$project_dir" ]]; then compose down --volumes; fi
}
cleanup() {
  cleanup_stack
  git -C "$root" worktree remove --force "$tmp/original" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT
git -C "$root" worktree add --detach "$tmp/original" "$base"
for variant in original batched; do
  project_dir="$tmp/original"
  if [[ "$variant" == batched ]]; then project_dir="$root"; fi
  compose up -d --wait --build api
  compose run --no-deps -d --name "$COMPOSE_PROJECT_NAME-peer" -p 127.0.0.1:8082:8081 api
  curl -fsS --retry 15 --retry-all-errors --retry-delay 1 http://127.0.0.1:8082/v1/ready
  sql() { compose exec -T postgres psql -XqAt -v ON_ERROR_STOP=1 -U "${DB_USER:-echostream}" -d "${DB_NAME:-echostream}" -c "$1"; }
  sql 'CREATE EXTENSION pg_stat_statements'
  sql 'SELECT pg_stat_statements_reset()'
  BENCH_COMMIT=$(git -C "$project_dir" rev-parse HEAD) RESULT="$root/membership-$variant.json" \
    node "$root/scripts/fanout-load.mjs" http://127.0.0.1:8081 http://127.0.0.1:8082
  # Include subscription/presence checks as well as message delivery checks.
  sql "SELECT coalesce(sum(calls),0)::bigint FROM pg_stat_statements
    WHERE query LIKE 'SELECT EXISTS (%FROM channel_members m%JOIN channels c%JOIN users u%'
       OR query LIKE 'SELECT k.tenant, k.channel, k.member%FROM unnest(%'" \
    | tee "$root/membership-$variant-calls.txt"
  cleanup_stack
  project_dir=
done
