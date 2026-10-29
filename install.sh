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
#   ./install.sh --shell-init       # print PATH + completion block for $SHELL
#   ./install.sh --migrate          # detect and clean up legacy install paths
#   ./install.sh -h|--help          # show this help

set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
SKIP_BUILD=0
VERBOSE=0
MODE="install"  # install | shell-init | migrate

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

# ── option parsing ────────────────────────────────────────────────────────────

for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    --verbose|-v) VERBOSE=1 ;;
    --shell-init) MODE="shell-init" ;;
    --migrate)    MODE="migrate" ;;
    -h|--help)
      sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      printf "%sinstall.sh:%s unknown argument: %s\n" "$C_ERR" "$C_RESET" "$arg" >&2
      exit 2
      ;;
  esac
done

# Login shell family (zsh, bash, fish); anything else gets bash syntax.
USER_SHELL="$(basename "${SHELL:-bash}")"

# ── repo root ─────────────────────────────────────────────────────────────────

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

# ── --shell-init mode ─────────────────────────────────────────────────────────
# Prints a shell profile block for $SHELL, delimited by BEGIN/END REX markers,
# and exits. Pick another shell with e.g. SHELL=bash ./install.sh --shell-init.
# Usage: ./install.sh --shell-init >> ~/.zshrc
#        ./install.sh --shell-init >> ~/.bashrc
#        ./install.sh --shell-init >> ~/.config/fish/config.fish

if [[ "$MODE" == "shell-init" ]]; then
  # Write $HOME literally when the bin dir is under it.
  _bin="${BIN_DIR/#"$HOME"/\$HOME}"
  case "$USER_SHELL" in
    zsh)
      # compdef exists only after compinit; skip completion rather than
      # erroring on every shell start when compinit has not run.
      cat <<SHELLBLOCK
# BEGIN REX
export PATH="$_bin:\$PATH"
if (( \$+functions[compdef] )) && command -v rex >/dev/null 2>&1; then
  source <(rex completion zsh)
fi
# END REX
SHELLBLOCK
      ;;
    fish)
      cat <<SHELLBLOCK
# BEGIN REX
contains -- "$_bin" \$PATH; or set -gx PATH "$_bin" \$PATH
type -q rex; and rex completion fish | source
# END REX
SHELLBLOCK
      ;;
    *)
      # eval, not source <(...): bash 3.2 (macOS /bin/bash) cannot source a
      # process substitution.
      cat <<SHELLBLOCK
# BEGIN REX
export PATH="$_bin:\$PATH"
if command -v rex >/dev/null 2>&1; then
  eval "\$(rex completion bash)"
fi
# END REX
SHELLBLOCK
      ;;
  esac
  exit 0
fi

# ── --migrate mode ────────────────────────────────────────────────────────────

if [[ "$MODE" == "migrate" ]]; then
  found_any=0

  # Legacy state directory.
  if [[ -d "$HOME/.rex" ]]; then
    found_any=1
    printf "%s⚠%s  found legacy state at %s~/.rex/%s\n" \
      "$C_WARN" "$C_RESET" "$C_BOLD" "$C_RESET"
    printf "   move to ~/.local/state/rex/? [y/N] "
    read -r _ans </dev/tty
    case "$_ans" in
      [yY]*)
        mkdir -p "$HOME/.local/state/rex"
        mv "$HOME/.rex" "$HOME/.local/state/rex"
        printf "   %s✓%s moved ~/.rex → ~/.local/state/rex\n" "$C_OK" "$C_RESET"
        ;;
      *)
        printf "   skipped\n"
        ;;
    esac
  fi

  # Old binary at /usr/local/bin/rex that differs from our install target.
  _legacy_bin="/usr/local/bin/rex"
  _install_target="$BIN_DIR/rex"
  if [[ -x "$_legacy_bin" && "$_legacy_bin" != "$_install_target" ]]; then
    found_any=1
    printf "%s⚠%s  found old rex binary at %s%s%s\n" \
      "$C_WARN" "$C_RESET" "$C_BOLD" "$_legacy_bin" "$C_RESET"
    printf "   remove it? [y/N] "
    read -r _ans </dev/tty
    case "$_ans" in
      [yY]*)
        rm -f "$_legacy_bin"
        printf "   %s✓%s removed %s\n" "$C_OK" "$C_RESET" "$_legacy_bin"
        ;;
      *)
        printf "   skipped\n"
        ;;
    esac
  fi

  if [[ $found_any -eq 0 ]]; then
    printf "  %sno legacy artifacts to migrate%s\n" "$C_MUTED" "$C_RESET"
  fi
  exit 0
fi

# ── OS / arch sanity check ────────────────────────────────────────────────────

_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
_arch="$(uname -m)"

case "$_os" in
  darwin|linux) ;;
  *)
    printf "%serror:%s rex builds on darwin/linux on amd64/arm64. Detected: %s/%s. File a request at https://github.com/tristanbietsch/rex/issues.\n" \
      "$C_ERR" "$C_RESET" "$_os" "$_arch" >&2
    exit 1
    ;;
esac

case "$_arch" in
  x86_64|arm64|aarch64) ;;
  *)
    printf "%serror:%s rex builds on darwin/linux on amd64/arm64. Detected: %s/%s. File a request at https://github.com/tristanbietsch/rex/issues.\n" \
      "$C_ERR" "$C_RESET" "$_os" "$_arch" >&2
    exit 1
    ;;
esac

# ── banner ────────────────────────────────────────────────────────────────────

GIT_VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || true)"
# Stamp `rex --version`; without git the binary keeps its built-in version.
LDFLAGS=""
if [[ -n "$GIT_VERSION" ]]; then
  LDFLAGS="-X github.com/tristanbietsch/rex/internal/surface/cli/core.Version=$GIT_VERSION"
fi
printf "\n  %s∴ rex installer%s  %s%s%s\n" \
  "$C_BOLD" "$C_RESET" "$C_MUTED" "${GIT_VERSION:-(from source)}" "$C_RESET"

# ── temp files ────────────────────────────────────────────────────────────────

# LOG captures work()'s output in spinner mode and is shown on failure.
# META carries KEY=1 flags out of the backgrounded work(); it is read with
# grep, never sourced.
LOG="$(mktemp -t rex-install.XXXXXX)"
META="$(mktemp -t rex-install-meta.XXXXXX)"
trap 'rm -f "$LOG" "$META"; printf "\033[?25h"' EXIT

# ── install pipeline ──────────────────────────────────────────────────────────
# work() runs every fallible step. Return codes propagate; set -e is suppressed
# when work() is called inside an `if !` test (that's intentional).
# Steps write to stdout/stderr only: the caller streams them (--verbose) or
# redirects them into $LOG (spinner). Writing to $LOG from inside work() would
# race the caller's redirect and clobber the log.

# version_ge A B: true when dotted version A >= B (missing parts count as 0).
version_ge() {
  local a b i
  IFS=. read -r -a a <<<"$1"
  IFS=. read -r -a b <<<"$2"
  for i in 0 1 2; do
    if (( 10#${a[i]:-0} > 10#${b[i]:-0} )); then return 0; fi
    if (( 10#${a[i]:-0} < 10#${b[i]:-0} )); then return 1; fi
  done
  return 0
}

work() {
  if [[ $SKIP_BUILD -eq 0 ]]; then
    local need have
    need="$(sed -n 's/^go \([0-9][0-9.]*\)$/\1/p' go.mod)"
    if ! command -v go >/dev/null 2>&1; then
      printf "%serror:%s go toolchain not found. Install Go %s+: https://go.dev/dl/\n" \
        "$C_ERR" "$C_RESET" "$need" >&2
      return 1
    fi
    # GOVERSION is the toolchain go builds with here: under the default
    # GOTOOLCHAIN=auto an older go switches to (and may download) go.mod's.
    # Forms: go1.26.10, go1.27rc1, "devel go1.28-abc… …".
    have="$(go env GOVERSION 2>/dev/null || true)"
    have="${have#devel }"; have="${have%% *}"; have="${have#go}"
    have="${have%%[!0-9.]*}"
    if [[ -z "$have" ]] || ! version_ge "$have" "$need"; then
      printf "%serror:%s rex needs Go %s+ (found %s). Install it: https://go.dev/dl/\n" \
        "$C_ERR" "$C_RESET" "$need" "${have:-unknown}" >&2
      return 1
    fi
    echo "→ building rex"
    if ! go build -ldflags "$LDFLAGS" -o rex ./cmd/rex; then
      printf "%serror:%s build of rex failed\n" "$C_ERR" "$C_RESET" >&2
      return 1
    fi
    echo "→ building rex-daemon"
    if ! go build -ldflags "$LDFLAGS" -o rex-daemon ./cmd/rex-daemon; then
      printf "%serror:%s build of rex-daemon failed\n" "$C_ERR" "$C_RESET" >&2
      return 1
    fi
  else
    [[ -x ./rex && -x ./rex-daemon ]] || {
      printf "%serror:%s --skip-build set but ./rex or ./rex-daemon is missing\n" \
        "$C_ERR" "$C_RESET" >&2
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
    echo "DAEMON_WAS_RUNNING=1" >>"$META"
  fi

  mkdir -p "$BIN_DIR" || return 1
  echo "→ installing to $BIN_DIR"
  if ! install -m 0755 rex "$BIN_DIR/rex"; then
    printf "%serror:%s cannot write to %s. Use PREFIX=/usr/local with sudo, or pick another prefix.\n" \
      "$C_ERR" "$C_RESET" "$BIN_DIR" >&2
    return 1
  fi
  if ! install -m 0755 rex-daemon "$BIN_DIR/rex-daemon"; then
    printf "%serror:%s cannot write to %s. Use PREFIX=/usr/local with sudo, or pick another prefix.\n" \
      "$C_ERR" "$C_RESET" "$BIN_DIR" >&2
    return 1
  fi
}

# ── spinner / verbose runner ──────────────────────────────────────────────────

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

# ── post-install output ───────────────────────────────────────────────────────

INSTALLED_VERSION="$("$BIN_DIR/rex" --version 2>/dev/null || echo "?")"

DAEMON_WAS_RUNNING=0
if grep -qx 'DAEMON_WAS_RUNNING=1' "$META" 2>/dev/null; then
  DAEMON_WAS_RUNNING=1
fi

printf "  %s✓%s %srex %s%s installed%s\n" \
  "$C_OK" "$C_RESET" "$C_BOLD" "$INSTALLED_VERSION" "$C_RESET" "$C_RESET"
printf "  %s→%s %s\n" "$C_MUTED" "$C_RESET" "$BIN_DIR/rex"
printf "  %s→%s %s\n" "$C_MUTED" "$C_RESET" "$BIN_DIR/rex-daemon"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    # Displayed, not expanded.
    # shellcheck disable=SC2088
    case "$USER_SHELL" in
      zsh)  _rc="~/.zshrc" ;;
      fish) _rc="~/.config/fish/config.fish" ;;
      *)    _rc="~/.bashrc" ;;
    esac
    printf "\n  %s⚠%s %s is not on \$PATH\n" "$C_WARN" "$C_RESET" "$BIN_DIR"
    if [[ "$USER_SHELL" == "fish" ]]; then
      printf "    %sset -gx PATH \"%s\" \$PATH%s\n" "$C_MUTED" "$BIN_DIR" "$C_RESET"
    else
      printf "    %sexport PATH=\"%s:\$PATH\"%s\n" "$C_MUTED" "$BIN_DIR" "$C_RESET"
    fi
    printf "    %sor run: ./install.sh --shell-init >> %s%s\n" "$C_MUTED" "$_rc" "$C_RESET"
    ;;
esac

if [[ $DAEMON_WAS_RUNNING -eq 1 ]]; then
  printf "\n  %srun%s %s%srex%s%s — daemon will restart on the new binary%s\n" \
    "$C_MUTED" "$C_RESET" "$C_BOLD" "$C_ACCENT" "$C_RESET" "$C_MUTED" "$C_RESET"
fi

# ── next-step hint ────────────────────────────────────────────────────────────

printf "\n"
if [[ ! -d "$HOME/.config/rex" ]]; then
  # First run — config directory does not exist yet.
  printf "  %snext:%s run %s%srex setup%s to configure rex\n\n" \
    "$C_MUTED" "$C_RESET" "$C_BOLD" "$C_ACCENT" "$C_RESET"
else
  # Upgrade — config already present.
  printf "  %snext:%s run %s%srex%s to launch\n\n" \
    "$C_MUTED" "$C_RESET" "$C_BOLD" "$C_ACCENT" "$C_RESET"
fi
