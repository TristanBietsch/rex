#!/bin/sh
# Build release archives of rex and rex-daemon.
#
# Usage:
#   tools/release/build.sh VERSION [OS/ARCH ...]
#
#   VERSION   the release tag, e.g. v2.0.0 (stamped into both binaries)
#   OS/ARCH   targets to build; default: the targets this host can build
#
# Output in $DIST (default ./dist):
#   rex_<ver>_<os>_<arch>.tar.gz   (darwin, linux)
#   rex_<ver>_windows_<arch>.zip
# Each archive holds rex, rex-daemon, LICENSE, README.md and CHANGELOG.md
# in a top-level folder of the same name. SHA256SUMS lists every archive
# in $DIST.
#
# Linux rex needs cgo (oto links ALSA), so linux targets build only on a
# linux host of the same arch, with libasound2-dev installed. darwin and
# windows build anywhere with CGO_ENABLED=0. rex-daemon is always static.
#
# The release workflow (.github/workflows/release.yml) runs this script;
# `make dist` runs it locally.

set -eu

if [ $# -lt 1 ]; then
  echo "usage: $0 VERSION [OS/ARCH ...]" >&2
  exit 2
fi

VERSION="$1"
shift

case "$VERSION" in
  v[0-9]*) ;;
  *)
    echo "build.sh: VERSION must look like v2.0.0 (got: $VERSION)" >&2
    exit 2
    ;;
esac

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DIST="${DIST:-$ROOT/dist}"
PKG="github.com/tristanbietsch/rex"
LDFLAGS="-s -w -X $PKG/internal/version.Version=$VERSION"
SEMVER="${VERSION#v}"

host_os="$(go env GOHOSTOS)"
host_arch="$(go env GOHOSTARCH)"

if [ $# -eq 0 ]; then
  set -- darwin/amd64 darwin/arm64 windows/amd64
  if [ "$host_os" = linux ]; then
    set -- "$@" "linux/$host_arch"
  fi
fi

mkdir -p "$DIST"
cd "$ROOT"

for target in "$@"; do
  os="${target%/*}"
  arch="${target#*/}"
  name="rex_${SEMVER}_${os}_${arch}"
  stage="$DIST/$name"
  ext=""
  [ "$os" = windows ] && ext=".exe"

  # Only linux rex needs cgo, and only natively.
  rex_cgo=0
  if [ "$os" = linux ]; then
    if [ "$host_os" != linux ] || [ "$host_arch" != "$arch" ]; then
      echo "build.sh: $target needs a linux/$arch host (rex links ALSA via cgo)" >&2
      exit 1
    fi
    rex_cgo=1
  fi

  echo "→ $target"
  rm -rf "$stage"
  mkdir -p "$stage"
  CGO_ENABLED=$rex_cgo GOOS=$os GOARCH=$arch \
    go build -trimpath -ldflags "$LDFLAGS" -o "$stage/rex$ext" ./cmd/rex
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
    go build -trimpath -ldflags "$LDFLAGS" -o "$stage/rex-daemon$ext" ./cmd/rex-daemon
  for f in LICENSE README.md CHANGELOG.md; do
    [ -f "$f" ] && cp "$f" "$stage/"
  done

  (
    cd "$DIST"
    if [ "$os" = windows ]; then
      rm -f "$name.zip"
      zip -qr "$name.zip" "$name"
    else
      rm -f "$name.tar.gz"
      tar -czf "$name.tar.gz" "$name"
    fi
  )
  rm -rf "$stage"
done

# Checksums over every archive in $DIST, so builds from several hosts can
# be collected into one folder and summed once.
(
  cd "$DIST"
  if command -v sha256sum >/dev/null 2>&1; then
    sum="sha256sum"
  else
    sum="shasum -a 256"
  fi
  # shellcheck disable=SC2086
  ls rex_*.tar.gz rex_*.zip 2>/dev/null | LC_ALL=C sort | xargs $sum > SHA256SUMS
)
echo "→ $DIST/SHA256SUMS"
