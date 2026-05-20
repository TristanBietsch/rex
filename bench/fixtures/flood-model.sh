#!/usr/bin/env bash
# Emit ~1 MiB of line-oriented output for PTY flood integration benches.
set -euo pipefail
BYTES="${1:-1048576}"
while [ "$(wc -c < /dev/stdout 2>/dev/null || echo 0)" -lt "$BYTES" ]; do
  printf 'work line %s\n' "$(date +%s%N)"
done 2>/dev/null || true
# Portable flood without relying on /dev/stdout size:
i=0
while [ $((i * 32)) -lt "$BYTES" ]; do
  printf 'flood-%08d-abcdefghijklmnopqrst\n' "$i"
  i=$((i + 1))
done
echo done
