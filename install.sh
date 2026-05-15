#!/usr/bin/env bash
# Build and install rex + rex-daemon into $PREFIX/bin (default: ~/.local/bin).
# If a daemon is already running, stop it before swapping the binaries,
# then leave it stopped — the next `rex` invocation will start it again.
#
# Usage:
#   ./install.sh                    # installs to ~/.local/bin
#   PREFIX=/usr/local ./install.sh  # installs to /usr/local/bin (may need sudo)
#   ./install.sh --skip-build       # just copy existing binaries (CI use)

set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
SKIP_BUILD=0

# Colors / glyphs — disable on non-TTY or NO_COLOR.
if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  C_RESET=$'\033[0m'
  C_DIM=$'\033[2m'
  C_BOLD=$'\033[1m'
  C_FG=$'\033[38;5;231m'
  C_ACCENT=$'\033[38;5;110m'
  C_OK=$'\033[38;5;114m'
  C_WARN=$'\033[38;5;179m'
  C_MUTED=$'\033[38;5;244m'
else
  C_RESET=""; C_DIM=""; C_BOLD=""; C_FG=""
  C_ACCENT=""; C_OK=""; C_WARN=""; C_MUTED=""
fi

G_ARROW="▸"
G_CHECK="✓"
G_WARN="⚠"
G_STAR="✦"
G_BULLET="·"
G_HR="────────────────────────────────────────────"

step()  { printf "%s%s%s %s%s%s\n" "$C_ACCENT" "$G_ARROW" "$C_RESET" "$C_FG" "$*" "$C_RESET"; }
ok()    { printf "%s%s%s %s\n" "$C_OK" "$G_CHECK" "$C_RESET" "$*"; }
warn()  { printf "%s%s%s %s\n" "$C_WARN" "$G_WARN" "$C_RESET" "$*"; }
muted() { printf "%s%s%s\n" "$C_MUTED" "$*" "$C_RESET"; }
hr()    { printf "%s%s%s\n" "$C_DIM" "$G_HR" "$C_RESET"; }

for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    -h|--help)
      sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      printf "%sinstall.sh:%s unknown argument: %s\n" "$C_WARN" "$C_RESET" "$arg" >&2
      exit 2
      ;;
  esac
done

if [[ $SKIP_BUILD -eq 0 ]] && ! command -v go >/dev/null 2>&1; then
  warn "go toolchain not found — install Go 1.22+ first"
  muted "  https://go.dev/dl/"
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

printf "\n"
printf "  %s%s rex installer%s\n" "$C_BOLD" "$G_STAR" "$C_RESET"
hr

if [[ $SKIP_BUILD -eq 0 ]]; then
  step "building rex + rex-daemon"
  go build -o rex ./cmd/rex
  go build -o rex-daemon ./cmd/rex-daemon
  ok "built binaries"
else
  if [[ ! -x ./rex || ! -x ./rex-daemon ]]; then
    warn "--skip-build set but ./rex or ./rex-daemon is missing"
    exit 1
  fi
  muted "  $G_BULLET skipping build (--skip-build)"
fi

# Stop any running daemon so the new binary is what next `rex` runs.
# Use the already-installed `rex` if available; otherwise the freshly-built one.
DAEMON_WAS_RUNNING=0
if [[ -x "$BIN_DIR/rex" ]]; then
  REX_FOR_STOP="$BIN_DIR/rex"
elif [[ -x ./rex ]]; then
  REX_FOR_STOP="./rex"
else
  REX_FOR_STOP=""
fi
if [[ -n "$REX_FOR_STOP" ]] && "$REX_FOR_STOP" daemon status >/dev/null 2>&1; then
  step "stopping running rex-daemon"
  "$REX_FOR_STOP" daemon stop >/dev/null 2>&1 || true
  DAEMON_WAS_RUNNING=1
  ok "daemon stopped"
fi

mkdir -p "$BIN_DIR"
step "installing to $C_ACCENT$BIN_DIR$C_RESET"
install -m 0755 rex "$BIN_DIR/rex"
install -m 0755 rex-daemon "$BIN_DIR/rex-daemon"
ok "binaries installed"

INSTALLED_VERSION="$("$BIN_DIR/rex" --version 2>/dev/null || echo "?")"

printf "\n"
hr
printf "  %s%s rex %s%s installed%s\n" "$C_BOLD" "$G_CHECK" "$INSTALLED_VERSION" "$C_FG" "$C_RESET"
printf "    %s$G_BULLET$C_RESET %s\n" "$C_MUTED" "$BIN_DIR/rex"
printf "    %s$G_BULLET$C_RESET %s\n" "$C_MUTED" "$BIN_DIR/rex-daemon"
hr
printf "\n"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    warn "$BIN_DIR is not on your PATH"
    muted "    add to your shell rc:  export PATH=\"$BIN_DIR:\$PATH\""
    printf "\n"
    ;;
esac

if [[ $DAEMON_WAS_RUNNING -eq 1 ]]; then
  printf "  %srun%s %s%srex%s%s — daemon will restart on the new binary.%s\n" \
    "$C_FG" "$C_RESET" "$C_BOLD" "$C_ACCENT" "$C_RESET" "$C_MUTED" "$C_RESET"
else
  printf "  %srun%s %s%srex%s %sto launch.%s\n" \
    "$C_FG" "$C_RESET" "$C_BOLD" "$C_ACCENT" "$C_RESET" "$C_MUTED" "$C_RESET"
fi
printf "\n"
