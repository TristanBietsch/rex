#!/bin/sh
# Install rex + rex-daemon into $PREFIX/bin (default: ~/.local/bin).
#
# By default this downloads the latest GitHub release for your OS and arch,
# verifies it against the release's SHA256SUMS, and installs both binaries.
# No sudo, no shell profile edits. Download it, read it, then run it:
#
#   curl -fsSL https://raw.githubusercontent.com/TristanBietsch/rex/master/install.sh -o install.sh
#   sh install.sh
#
# Usage:
#   sh install.sh                     # latest release into ~/.local/bin
#   REX_VERSION=v2.0.0 sh install.sh  # a specific release
#   PREFIX=/opt/rex sh install.sh     # installs to /opt/rex/bin
#   sh install.sh --from-source       # build with Go (this checkout, or a clone)
#   sh install.sh --skip-build        # copy ./rex and ./rex-daemon (CI use)
#   sh install.sh --verbose           # show build output
#   sh install.sh --shell-init        # print a shell profile block and exit
#   sh install.sh --migrate           # detect and clean up legacy install paths
#   sh install.sh -h|--help           # show this help
#
# A running daemon is left running on the old binary, so your sessions keep
# going; the script prints how to switch it to the new one.

set -eu

REPO="TristanBietsch/rex"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
# REX_DOWNLOAD_URL points at a mirror of the release downloads (it must
# have <url>/latest/download/<file> and <url>/download/<tag>/<file>).
# Only https:// (and file:// for testing) is accepted.
DOWNLOAD_URL="${REX_DOWNLOAD_URL:-https://github.com/$REPO/releases}"
REX_VERSION="${REX_VERSION:-}"
FROM_SOURCE=0
SKIP_BUILD=0
VERBOSE=0
MODE="install"  # install | shell-init | migrate

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_RESET="$(printf '\033[0m')"
  C_BOLD="$(printf '\033[1m')"
  C_OK="$(printf '\033[38;5;114m')"
  C_ERR="$(printf '\033[38;5;167m')"
  C_WARN="$(printf '\033[38;5;179m')"
  C_MUTED="$(printf '\033[38;5;244m')"
  C_ACCENT="$(printf '\033[38;5;110m')"
else
  C_RESET=""; C_BOLD=""; C_OK=""; C_ERR=""; C_WARN=""; C_MUTED=""; C_ACCENT=""
fi

die() {
  printf "%serror:%s %s\n" "$C_ERR" "$C_RESET" "$*" >&2
  exit 1
}

step() {
  printf "  %s→%s %s\n" "$C_MUTED" "$C_RESET" "$*"
}

# ── option parsing ────────────────────────────────────────────────────────────

for arg in "$@"; do
  case "$arg" in
    --from-source) FROM_SOURCE=1 ;;
    --skip-build)  FROM_SOURCE=1; SKIP_BUILD=1 ;;
    --verbose|-v)  VERBOSE=1 ;;
    --shell-init)  MODE="shell-init" ;;
    --migrate)     MODE="migrate" ;;
    -h|--help)
      sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      printf "%sinstall.sh:%s unknown argument: %s\n" "$C_ERR" "$C_RESET" "$arg" >&2
      exit 2
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ── --shell-init mode ─────────────────────────────────────────────────────────
# Prints an idempotent shell profile block and exits. It never edits a file.
# Usage: sh install.sh --shell-init >> ~/.zshrc

if [ "$MODE" = "shell-init" ]; then
  cat <<'SHELLBLOCK'
# BEGIN REX
export PATH="$HOME/.local/bin:$PATH"
source <(rex completion bash 2>/dev/null) || true
# END REX
SHELLBLOCK
  if [ "$(basename "${SHELL:-sh}")" = "fish" ]; then
    printf "\n# fish users: add this to ~/.config/fish/config.fish instead:\n"
    printf "#   set -gx PATH \"\$HOME/.local/bin\" \$PATH\n"
    printf "#   rex completion fish | source\n"
  fi
  exit 0
fi

# ── --migrate mode ────────────────────────────────────────────────────────────

if [ "$MODE" = "migrate" ]; then
  found_any=0

  if [ -d "$HOME/.rex" ]; then
    found_any=1
    printf "%s⚠%s  found legacy state at %s~/.rex/%s\n" "$C_WARN" "$C_RESET" "$C_BOLD" "$C_RESET"
    printf "   move to ~/.local/state/rex/? [y/N] "
    read -r _ans </dev/tty
    case "$_ans" in
      [yY]*)
        mkdir -p "$HOME/.local/state/rex"
        mv "$HOME/.rex" "$HOME/.local/state/rex"
        printf "   %s✓%s moved ~/.rex → ~/.local/state/rex\n" "$C_OK" "$C_RESET"
        ;;
      *) printf "   skipped\n" ;;
    esac
  fi

  _legacy_bin="/usr/local/bin/rex"
  if [ -x "$_legacy_bin" ] && [ "$_legacy_bin" != "$BIN_DIR/rex" ]; then
    found_any=1
    printf "%s⚠%s  found old rex binary at %s%s%s\n" "$C_WARN" "$C_RESET" "$C_BOLD" "$_legacy_bin" "$C_RESET"
    printf "   remove it? [y/N] "
    read -r _ans </dev/tty
    case "$_ans" in
      [yY]*)
        rm -f "$_legacy_bin"
        printf "   %s✓%s removed %s\n" "$C_OK" "$C_RESET" "$_legacy_bin"
        ;;
      *) printf "   skipped\n" ;;
    esac
  fi

  if [ "$found_any" -eq 0 ]; then
    printf "  %sno legacy artifacts to migrate%s\n" "$C_MUTED" "$C_RESET"
  fi
  exit 0
fi

# ── platform ──────────────────────────────────────────────────────────────────

_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
_arch="$(uname -m)"

case "$_os" in
  darwin|linux) ;;
  *)
    die "this installer supports macOS and Linux (detected $_os/$_arch). On Windows use install.ps1 or the .zip from https://github.com/$REPO/releases."
    ;;
esac

case "$_arch" in
  x86_64|amd64) _arch="amd64" ;;
  arm64|aarch64) _arch="arm64" ;;
  *) die "rex ships amd64 and arm64 builds (detected $_os/$_arch). Try --from-source." ;;
esac

# An x86_64 shell under Rosetta on Apple silicon: install the native build.
if [ "$_os" = darwin ] && [ "$_arch" = amd64 ] &&
   [ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" = 1 ]; then
  _arch="arm64"
fi

# ── temp dir ──────────────────────────────────────────────────────────────────

TMP="$(mktemp -d "${TMPDIR:-/tmp}/rex-install.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
trap 'exit 130' INT TERM

# ── release download ──────────────────────────────────────────────────────────

# fetch URL OUT downloads over https only (file:// when the mirror is local).
fetch() {
  case "$1" in
    https://*) _proto="=https" ;;
    file://*)  _proto="=file" ;;
    *) die "refusing to download over a non-https URL: $1" ;;
  esac
  if command -v curl >/dev/null 2>&1; then
    curl --proto "$_proto" --proto-redir "=https" --tlsv1.2 -fsSL --retry 3 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    case "$1" in
      https://*) wget --https-only -q -O "$2" "$1" ;;
      *) die "wget cannot read $1; install curl" ;;
    esac
  else
    die "need curl or wget to download rex (or use --from-source)"
  fi
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    die "need sha256sum or shasum to verify the download"
  fi
}

download_release() {
  case "$DOWNLOAD_URL" in
    https://*|file://*) ;;
    *) die "REX_DOWNLOAD_URL must start with https:// (got: $DOWNLOAD_URL)" ;;
  esac

  if [ -n "$REX_VERSION" ]; then
    case "$REX_VERSION" in
      v*) ;;
      *) REX_VERSION="v$REX_VERSION" ;;
    esac
    case "$REX_VERSION" in
      *[!A-Za-z0-9._-]*) die "REX_VERSION has unexpected characters: $REX_VERSION" ;;
    esac
    _base="$DOWNLOAD_URL/download/$REX_VERSION"
    _which="$REX_VERSION"
  else
    _base="$DOWNLOAD_URL/latest/download"
    _which="latest release"
  fi

  step "fetching checksums ($_which)"
  fetch "$_base/SHA256SUMS" "$TMP/SHA256SUMS" ||
    die "could not download $_base/SHA256SUMS (no such release, or offline?)"

  # The archive for this platform: rex_<ver>_<os>_<arch>.tar.gz
  _line="$(grep -E "^[0-9a-f]{64}  rex_[0-9A-Za-z.+-]+_${_os}_${_arch}\.tar\.gz\$" "$TMP/SHA256SUMS" | head -n 1 || true)"
  [ -n "$_line" ] || die "the release has no build for $_os/$_arch. Try --from-source."
  _want="${_line%% *}"
  _file="${_line##* }"
  _dir="${_file%.tar.gz}"
  _ver="${_dir#rex_}"
  _ver="v${_ver%_"${_os}"_"${_arch}"}"

  step "downloading $_file"
  fetch "$_base/$_file" "$TMP/$_file" || die "could not download $_base/$_file"

  _got="$(sha256_of "$TMP/$_file")"
  if [ "$_got" != "$_want" ]; then
    die "checksum mismatch for $_file (want $_want, got $_got). Nothing was installed."
  fi
  step "checksum ok ($_want)"

  tar -xzf "$TMP/$_file" -C "$TMP"
  [ -f "$TMP/$_dir/rex" ] && [ -f "$TMP/$_dir/rex-daemon" ] ||
    die "$_file does not contain rex and rex-daemon"
  SRC_REX="$TMP/$_dir/rex"
  SRC_DAEMON="$TMP/$_dir/rex-daemon"
  INSTALL_LABEL="$_ver"
}

# ── source build ──────────────────────────────────────────────────────────────

build_from_source() {
  if [ -f "$SCRIPT_DIR/go.mod" ] && [ -d "$SCRIPT_DIR/cmd/rex" ]; then
    _src="$SCRIPT_DIR"
  else
    command -v git >/dev/null 2>&1 || die "--from-source outside a checkout needs git"
    _ref="${REX_VERSION:-master}"
    step "cloning $REPO ($_ref)"
    git clone --quiet --depth 1 --branch "$_ref" "https://github.com/$REPO.git" "$TMP/src" ||
      die "git clone of $_ref failed"
    _src="$TMP/src"
  fi

  if [ "$SKIP_BUILD" -eq 1 ]; then
    [ -x "$_src/rex" ] && [ -x "$_src/rex-daemon" ] ||
      die "--skip-build set but $_src/rex or $_src/rex-daemon is missing"
    SRC_REX="$_src/rex"
    SRC_DAEMON="$_src/rex-daemon"
    INSTALL_LABEL="(prebuilt)"
    return
  fi

  command -v go >/dev/null 2>&1 || die "go toolchain not found. Install the Go version in go.mod: https://go.dev/dl/"
  _ver="$(git -C "$_src" describe --tags --always --dirty 2>/dev/null || echo dev)"
  _ld="-X github.com/tristanbietsch/rex/internal/version.Version=$_ver"
  mkdir -p "$TMP/build"
  for _cmd in rex rex-daemon; do
    step "building $_cmd ($_ver)"
    if [ "$VERBOSE" -eq 1 ]; then
      (cd "$_src" && go build -trimpath -ldflags "$_ld" -o "$TMP/build/$_cmd" "./cmd/$_cmd")
    elif ! (cd "$_src" && go build -trimpath -ldflags "$_ld" -o "$TMP/build/$_cmd" "./cmd/$_cmd") >"$TMP/build.log" 2>&1; then
      tail -20 "$TMP/build.log" >&2
      die "build of $_cmd failed"
    fi
  done
  SRC_REX="$TMP/build/rex"
  SRC_DAEMON="$TMP/build/rex-daemon"
  INSTALL_LABEL="$_ver"
}

# ── install ───────────────────────────────────────────────────────────────────

printf "\n  %s∴ rex installer%s\n\n" "$C_BOLD" "$C_RESET"

if [ "$FROM_SOURCE" -eq 1 ]; then
  build_from_source
else
  download_release
fi

# Was a daemon running before the swap? It keeps running the old binary.
DAEMON_WAS_RUNNING=0
if [ -x "$BIN_DIR/rex" ] && REX_AGENT=0 "$BIN_DIR/rex" daemon status >/dev/null 2>&1; then
  DAEMON_WAS_RUNNING=1
fi

mkdir -p "$BIN_DIR" || die "cannot create $BIN_DIR. Pick another PREFIX."
step "installing to $BIN_DIR"
# install(1) writes a new file, so a running daemon keeps its old binary.
install -m 0755 "$SRC_DAEMON" "$BIN_DIR/rex-daemon" ||
  die "cannot write to $BIN_DIR. Pick another PREFIX you own."
install -m 0755 "$SRC_REX" "$BIN_DIR/rex" ||
  die "cannot write to $BIN_DIR. Pick another PREFIX you own."

# ── post-install output ───────────────────────────────────────────────────────

INSTALLED_VERSION="$(REX_AGENT=0 REX_OUTPUT=txt "$BIN_DIR/rex" --version 2>/dev/null || echo "$INSTALL_LABEL")"

printf "\n  %s✓%s %srex %s installed%s\n" "$C_OK" "$C_RESET" "$C_BOLD" "$INSTALLED_VERSION" "$C_RESET"
printf "  %s→%s %s\n" "$C_MUTED" "$C_RESET" "$BIN_DIR/rex"
printf "  %s→%s %s\n" "$C_MUTED" "$C_RESET" "$BIN_DIR/rex-daemon"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    printf "\n  %s⚠%s %s is not on \$PATH. Add this to your shell profile:\n" "$C_WARN" "$C_RESET" "$BIN_DIR"
    printf "    %sexport PATH=\"%s:\$PATH\"%s\n" "$C_MUTED" "$BIN_DIR" "$C_RESET"
    ;;
esac

if [ "$DAEMON_WAS_RUNNING" -eq 1 ]; then
  printf "\n  %s⚠%s the running daemon still uses the old binary.\n" "$C_WARN" "$C_RESET"
  if REX_AGENT=0 "$BIN_DIR/rex" help upgrade >/dev/null 2>&1; then
    printf "    %srun%s %srex upgrade%s %sto switch (keeps sessions; asks first if the old daemon cannot hot-restart)%s\n" \
      "$C_MUTED" "$C_RESET" "$C_ACCENT" "$C_RESET" "$C_MUTED" "$C_RESET"
  else
    printf "    %srun%s %srex daemon restart%s %swhen your sessions are done (it ends them)%s\n" \
      "$C_MUTED" "$C_RESET" "$C_ACCENT" "$C_RESET" "$C_MUTED" "$C_RESET"
  fi
fi

printf "\n"
if [ ! -d "$HOME/.config/rex" ]; then
  printf "  %snext:%s run %srex onboard%s for a guided first run\n\n" "$C_MUTED" "$C_RESET" "$C_ACCENT" "$C_RESET"
else
  printf "  %snext:%s run %srex%s to launch\n\n" "$C_MUTED" "$C_RESET" "$C_ACCENT" "$C_RESET"
fi
