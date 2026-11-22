#!/bin/bash
# Run only the "up" migrations in order.
# Postgres initdb.d runs ALL files alphabetically, including .down.sql
# which breaks things. This script runs only what we want.
set -e

for f in /migrations/*.up.sql; do
  echo "Running migration: $f"
  psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f "$f"
done

