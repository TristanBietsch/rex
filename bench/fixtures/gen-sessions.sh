#!/usr/bin/env bash
# Write N synthetic session dirs under ROOT/sessions/<id>/meta.json for LoadAll benches.
set -euo pipefail

ROOT="${1:?usage: gen-sessions.sh ROOT COUNT}"
COUNT="${2:?usage: gen-sessions.sh ROOT COUNT}"

mkdir -p "$ROOT/sessions"

for i in $(seq 1 "$COUNT"); do
  id=$(printf "bench-%04d-0000-4000-8000-000000%08x" "$i")
  short=$(printf "%04x" "$i")
  dir="$ROOT/sessions/$id"
  mkdir -p "$dir"
  cat >"$dir/meta.json" <<EOF
{
  "id": "$id",
  "short_id": "$short",
  "tool_id": "echo",
  "model_id": "short",
  "slug": "bench-$i",
  "state": "done",
  "started_at": "2026-01-01T00:00:00Z",
  "last_event_at": "2026-01-01T01:00:00Z",
  "last_line": "completed bench session $i"
}
EOF
done

echo "wrote $COUNT sessions under $ROOT/sessions"
