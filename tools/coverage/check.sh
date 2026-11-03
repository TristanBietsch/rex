#!/bin/sh
# check.sh enforces per-package statement-coverage floors.
#
# Usage: tools/coverage/check.sh [baseline]
#
# Runs `go test -cover ./...` from the repo root, or parses the file named
# by $COVER_INPUT instead (saved output of that same command). Each package
# is compared with its floor in the baseline file (default:
# tools/coverage/baseline.txt). Baseline lines are
# `<import path> <min int percent>`; blank lines and # comments are ignored.
#
# Exit status:
#   0  every listed package meets its floor
#   1  a listed package is below its floor or missing, or go test failed
#   2  usage error or malformed baseline
#
# Packages that are not in the baseline are reported but never fail the
# check, so a new package can land before it has a floor. GOFLAGS is passed
# through to go test unchanged.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
baseline=${1:-$here/baseline.txt}

if [ ! -r "$baseline" ]; then
	echo "check.sh: cannot read baseline: $baseline" >&2
	exit 2
fi

out=$(mktemp)
trap 'rm -f "$out"' EXIT
trap 'exit 1' HUP INT TERM

if [ -n "${COVER_INPUT:-}" ]; then
	cp "$COVER_INPUT" "$out"
elif ! (cd "$root" && go test -cover ./...) >"$out" 2>&1; then
	cat "$out" >&2
	echo "check.sh: go test failed; coverage not checked" >&2
	exit 1
fi

awk -v baseline="$baseline" '
BEGIN {
	while ((getline line < baseline) > 0) {
		sub(/#.*/, "", line)
		n = split(line, f, " ")
		if (n == 0)
			continue
		if (n != 2 || f[2] !~ /^[0-9]+$/) {
			printf "check.sh: malformed baseline line: %s\n", line > "/dev/stderr"
			bad = 1
			continue
		}
		floor[f[1]] = f[2] + 0
		order[++nfloor] = f[1]
	}
	close(baseline)
	if (bad)
		exit 2
}

# go test -cover prints one of, per package:
#   ok  <pkg> <time|(cached)> coverage: 67.8% of statements
#       <pkg>                 coverage: 0.0% of statements   (no test files)
#   ok  <pkg> <time>          coverage: [no statements]
# A bare "coverage: ..." line (go test -v) starts with the keyword and is
# skipped because it names no package.
{
	for (i = 2; i < NF; i++) {
		if ($i != "coverage:")
			continue
		pkg = ($1 == "ok") ? $2 : $1
		v = $(i + 1)
		if (v ~ /^[0-9.]+%$/) {
			sub(/%$/, "", v)
			cov[pkg] = v + 0
		} else {
			cov[pkg] = "none"
		}
		break
	}
}

END {
	if (bad)
		exit 2
	row = "%-60s %9s %6s  %s\n"
	printf row, "PACKAGE", "COVERAGE", "FLOOR", "STATUS"
	for (k = 1; k <= nfloor; k++) {
		p = order[k]
		if (!(p in cov)) {
			printf row, p, "-", floor[p] "%", "MISSING"
			fail++
			continue
		}
		if (cov[p] == "none") {
			printf row, p, "n/a", floor[p] "%", "ok"
			continue
		}
		status = "ok"
		if (cov[p] < floor[p]) {
			status = "FAIL"
			fail++
		}
		printf row, p, sprintf("%.1f%%", cov[p]), floor[p] "%", status
	}
	sorter = "sort"
	for (p in cov) {
		if (p in floor)
			continue
		shown = (cov[p] == "none") ? "n/a" : sprintf("%.1f%%", cov[p])
		printf row, p, shown, "-", "unlisted" | sorter
		unlisted++
	}
	close(sorter)
	if (unlisted)
		printf "\n%d package(s) have no floor; add them to %s.\n", unlisted, baseline
	if (fail) {
		printf "\n%d package(s) below floor or missing.\n", fail
		exit 1
	}
}
' "$out"
