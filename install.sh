#!/usr/bin/env bash
# Build and install rex + rex-daemon into $PREFIX/bin (default: ~/.local/bin).
#
# Usage:
#   ./install.sh               # installs to ~/.local/bin
#   PREFIX=/usr/local ./install.sh   # installs to /usr/local/bin (may need sudo)

set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"

if ! command -v go >/dev/null 2>&1; then
  echo "rex install: go toolchain not found — install Go 1.22+ first" >&2
  echo "  https://go.dev/dl/" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

echo "==> building rex + rex-daemon"
go build -o rex ./cmd/rex
go build -o rex-daemon ./cmd/rex-daemon

mkdir -p "$BIN_DIR"
echo "==> installing to $BIN_DIR"
install -m 0755 rex "$BIN_DIR/rex"
install -m 0755 rex-daemon "$BIN_DIR/rex-daemon"

echo
echo "rex installed:"
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

echo "run \`rex\` to launch."
