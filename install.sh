#!/usr/bin/env bash
# Build and install rex + rex-daemon into $PREFIX/bin (default: ~/.local/bin).
# If a daemon is already running, stop it before swapping the binaries,
# then leave it stopped — the next `rex` invocation will start it again.
#
# Usage:
#   ./install.sh                    # installs to ~/.local/bin
#   PREFIX=/usr/local ./install.sh  # installs to /usr/local/bin (may need sudo)
#   ./install.sh --skip-build       # just copy existing binaries (CI use)
#   ./install.sh --verbose          # stream build/install output (no spinner)

set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
SKIP_BUILD=0
VERBOSE=0

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  C_RESET=$'\033[0m'
  C_BOLD=$'\033[1m'
  C_OK=$'\033[38;5;114m'
  C_ERR=$'\033[38;5;167m'
  C_WARN=$'\033[38;5;179m'
  C_MUTED=$'\033[38;5;244m'
  C_ACCENT=$'\033[38;5;110m'
  TTY=1
else
  C_RESET=""; C_BOLD=""; C_OK=""; C_ERR=""; C_WARN=""; C_MUTED=""; C_ACCENT=""
  TTY=0
fi

for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    --verbose|-v) VERBOSE=1 ;;
    -h|--help)
      sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      printf "%sinstall.sh:%s unknown argument: %s\n" "$C_ERR" "$C_RESET" "$arg" >&2
      exit 2
      ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

LOG="$(mktemp -t rex-install.XXXXXX)"
META="$LOG.meta"
trap 'rm -f "$LOG" "$META"; printf "\033[?25h"' EXIT

# work runs the install pipeline. Every fallible step is guarded with
# `|| return 1` because `set -e` is suppressed when work() runs inside
# an `if !` test (which is how we detect failure to swap to the error path).
work() {
  if [[ $SKIP_BUILD -eq 0 ]]; then
    command -v go >/dev/null 2>&1 || {
      echo "go toolchain not found — install Go 1.22+ (https://go.dev/dl/)" >&2
      return 1
    }
    echo "→ building rex"
    go build -o rex ./cmd/rex || return 1
    echo "→ building rex-daemon"
    go build -o rex-daemon ./cmd/rex-daemon || return 1
  else
    [[ -x ./rex && -x ./rex-daemon ]] || {
      echo "--skip-build set but ./rex or ./rex-daemon is missing" >&2
      return 1
    }
  fi

  local rex_for_stop=""
  if [[ -x "$BIN_DIR/rex" ]]; then
    rex_for_stop="$BIN_DIR/rex"
  elif [[ -x ./rex ]]; then
    rex_for_stop="./rex"
  fi
  if [[ -n "$rex_for_stop" ]] && "$rex_for_stop" daemon status >/dev/null 2>&1; then
    echo "→ stopping running daemon"
    "$rex_for_stop" daemon stop >/dev/null 2>&1 || true
    echo "DAEMON_WAS_RUNNING=1" >> "$META"
  fi

  mkdir -p "$BIN_DIR" || return 1
  echo "→ installing to $BIN_DIR"
  install -m 0755 rex "$BIN_DIR/rex" || return 1
  install -m 0755 rex-daemon "$BIN_DIR/rex-daemon" || return 1
}

# spin runs work() in the background, animating a braille spinner until done.
spin() {
  local frames=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")
  local i=0
  work >"$LOG" 2>&1 &
  local pid=$!
  printf '\033[?25l'
  while kill -0 "$pid" 2>/dev/null; do
    printf "\r  %s%s%s installing rex" "$C_ACCENT" "${frames[i]}" "$C_RESET"
    i=$(( (i + 1) % ${#frames[@]} ))
    sleep 0.08
  done
  printf '\033[?25h'
  if ! wait "$pid"; then
    printf "\r\033[K  %s✗%s install failed\n\n" "$C_ERR" "$C_RESET"
    sed 's/^/    /' "$LOG"
    printf "\n"
    return 1
  fi
  printf "\r\033[K"
}

printf "\n"
if [[ $VERBOSE -eq 1 || $TTY -eq 0 ]]; then
  if ! work; then
    printf "\n  %s✗%s install failed\n\n" "$C_ERR" "$C_RESET"
    exit 1
  fi
else
  spin || exit 1
fi

INSTALLED_VERSION="$("$BIN_DIR/rex" --version 2>/dev/null || echo "?")"

DAEMON_WAS_RUNNING=0
[[ -f "$META" ]] && source "$META"

printf "  %s✓%s %srex %s%s installed%s\n" \
  "$C_OK" "$C_RESET" "$C_BOLD" "$INSTALLED_VERSION" "$C_RESET" "$C_RESET"
printf "  %s→%s %s\n" "$C_MUTED" "$C_RESET" "$BIN_DIR/rex"
printf "  %s→%s %s\n" "$C_MUTED" "$C_RESET" "$BIN_DIR/rex-daemon"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    printf "\n  %s⚠%s %s is not on \$PATH\n" "$C_WARN" "$C_RESET" "$BIN_DIR"
    printf "    %sexport PATH=\"%s:\$PATH\"%s\n" "$C_MUTED" "$BIN_DIR" "$C_RESET"
    ;;
esac

if [[ ${DAEMON_WAS_RUNNING:-0} -eq 1 ]]; then
  printf "\n  %srun%s %s%srex%s%s — daemon will restart on the new binary%s\n\n" \
    "$C_MUTED" "$C_RESET" "$C_BOLD" "$C_ACCENT" "$C_RESET" "$C_MUTED" "$C_RESET"
else
  printf "\n  %srun%s %s%srex%s %sto launch%s\n\n" \
    "$C_MUTED" "$C_RESET" "$C_BOLD" "$C_ACCENT" "$C_RESET" "$C_MUTED" "$C_RESET"
fi
