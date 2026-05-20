#!/usr/bin/env bash
# Run unit benchmarks once and warn when results exceed budgets.txt by >10%.
set -euo pipefail
cd "$(dirname "$0")/.."

out=$(mktemp)
trap 'rm -f "$out"' EXIT

go test -bench='TranscriptFile|RenderBoard|Codec_|LoadAll' -benchmem -count=3 -timeout=30m \
  ./internal/daemon/state/... \
  ./internal/wire/protocol/... \
  ./internal/surface/tui/... \
  >"$out" 2>&1

fail=0
while IFS= read -r line; do
  [[ "$line" =~ ^#.*$ || -z "$line" ]] && continue
  bench=$(awk '{print $1}' <<<"$line")
  budget_ns=$(awk '{print $2}' <<<"$line")
  result=$(grep "^${bench}-" "$out" 2>/dev/null | tail -1 || true)
  [[ -z "$result" ]] && continue
  actual_ns=$(awk '{print $3}' <<<"$result" | sed 's/ns.*//')
  if [[ "$budget_ns" =~ ^[0-9]+$ && "$actual_ns" =~ ^[0-9]+$ ]]; then
    max=$((budget_ns * 110 / 100))
    if (( actual_ns > max )); then
      echo "REGRESSION: $bench ${actual_ns}ns/op (budget ${budget_ns}ns +10%)"
      fail=1
    fi
  fi
done <bench/budgets.txt

if (( fail )); then
  echo "see $out for full benchmark log"
  exit 1
fi
echo "budget check passed"
