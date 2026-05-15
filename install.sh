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

for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    -h|--help)
      sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "install.sh: unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

if [[ $SKIP_BUILD -eq 0 ]] && ! command -v go >/dev/null 2>&1; then
  echo "install.sh: go toolchain not found — install Go 1.22+ first" >&2
  echo "  https://go.dev/dl/" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

if [[ $SKIP_BUILD -eq 0 ]]; then
  echo "==> building rex + rex-daemon"
  go build -o rex ./cmd/rex
  go build -o rex-daemon ./cmd/rex-daemon
else
  if [[ ! -x ./rex || ! -x ./rex-daemon ]]; then
    echo "install.sh: --skip-build set but ./rex or ./rex-daemon is missing" >&2
    exit 1
  fi
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
  echo "==> stopping running rex-daemon"
  "$REX_FOR_STOP" daemon stop >/dev/null 2>&1 || true
  DAEMON_WAS_RUNNING=1
fi

mkdir -p "$BIN_DIR"
echo "==> installing to $BIN_DIR"
install -m 0755 rex "$BIN_DIR/rex"
install -m 0755 rex-daemon "$BIN_DIR/rex-daemon"

INSTALLED_VERSION="$("$BIN_DIR/rex" --version 2>/dev/null || echo "?")"

echo
echo "rex $INSTALLED_VERSION installed:"
echo "  $BIN_DIR/rex"
echo "  $BIN_DIR/rex-daemon"
echo

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    echo "note: $BIN_DIR is not on your PATH. Add this line to your shell rc:"
    echo "    export PATH=\"$BIN_DIR:\$PATH\""
    echo
    ;;
esac

if [[ $DAEMON_WAS_RUNNING -eq 1 ]]; then
  echo "the daemon was stopped — run \`rex\` to start it again on the new binary."
else
  echo "run \`rex\` to launch."
fi
