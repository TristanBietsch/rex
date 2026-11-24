#!/bin/sh
# Rebuild rex from this checkout, install the binaries that changed, and
# move the running daemon and boards onto the new build. Sessions survive.
#
# Usage:
#   tools/update/update.sh [--pull] [--no-restart] [-v] [-h]
#   make update [UPDATE_FLAGS=--pull]
#
#   --pull        git pull --ff-only first, then run the pulled script
#   --no-restart  install only; leave the running daemon and boards alone
#   -v            show the build output
#   PREFIX        install into $PREFIX/bin (default ~/.local, as make install)
#
# Safe while rex runs, even from inside a rex session: each binary is
# swapped with one rename(2), and the daemon is only ever hot-restarted
# (rex upgrade -no-cold). A binary or process already current is left alone.
#
# Exit: 0 done, 1 failed (nothing new installed), 2 usage,
#       3 installed, but the daemon is not on the new build (reason above),
#       75 another update is running.

set -eu

say() { printf 'update: %s\n' "$*"; }
warn() { printf 'update: warning: %s\n' "$*" >&2; }

# log appends one line to update.log; it never fails the run.
log() {
  [ -n "${LOG:-}" ] || return 0
  printf '%s pid=%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$$" "$*" >>"$LOG" 2>/dev/null || :
}

die() {
  printf 'update: %s\n' "$*" >&2
  log "error: $*"
  exit 1
}

usage() { sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; }

# cleanup runs on every exit: the staging folder goes, and the lock is
# released only while it still names this process.
cleanup() {
  if [ -n "${STAGE:-}" ]; then rm -rf "$STAGE"; fi
  if [ "${LOCK_HELD:-0}" -eq 1 ] && [ "$(readlink "$LOCK" 2>/dev/null)" = "$$" ]; then
    rm -f "$LOCK"
  fi
}

# The explicit exit in each signal trap makes dash run the EXIT trap too.
set_traps() {
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  trap 'exit 129' HUP
}

parse() {
  PULL=0 RESTART=1 VERBOSE=0
  for arg in "$@"; do
    case $arg in
      --pull) PULL=1 ;;
      --no-restart) RESTART=0 ;;
      -v | --verbose) VERBOSE=1 ;;
      -h | --help) usage; exit 0 ;;
      *) printf 'update: unknown argument: %s\n' "$arg" >&2; usage >&2; exit 2 ;;
    esac
  done
}

resolve_paths() {
  here=$(CDPATH='' cd -P -- "$(dirname -- "$0")" && pwd) || die "cannot find the folder of $0"
  SRC=$(CDPATH='' cd -P -- "$here/../.." && pwd) || die "cannot find the checkout above $here"
  if [ ! -f "$SRC/go.mod" ] || [ ! -d "$SRC/cmd/rex" ]; then
    die "$SRC is not a rex checkout; run tools/update/update.sh from inside one (not through a symlink)"
  fi
  PREFIX=${PREFIX:-$HOME/.local}
  case $PREFIX in /*) ;; *) PREFIX=$(pwd)/$PREFIX ;; esac
  BIN=$PREFIX/bin
  mkdir -p "$BIN" || die "cannot create $BIN; pick another PREFIX"
  logdir=${REX_LOG_DIR:-$HOME/.local/state/rex}
  if (umask 077 && mkdir -p "$logdir") 2>/dev/null; then LOG=$logdir/update.log; else LOG=; fi
}

# lock_stale says whether the PID in a lock no longer runs this script. A
# dead PID is stale. A live one is stale only when ps names another
# program (the PID was reused); when ps cannot tell, the lock is busy.
lock_stale() {
  case $1 in '' | *[!0-9]*) return 0 ;; esac
  kill -0 "$1" 2>/dev/null || return 0
  cmd=$(ps -p "$1" -o command= 2>/dev/null) || return 1
  [ -n "$cmd" ] || return 1
  case $cmd in *update.sh*) return 1 ;; esac
  return 0
}

# lock takes BIN/.rex-update.lock, a symlink whose target is the owner's
# PID: ln -s creates it atomically with the PID already in it. A lock that
# already names this PID means the script was re-exec'd after --pull.
lock() {
  LOCK=$BIN/.rex-update.lock
  LOCK_HELD=0 PULLED=0
  if ln -s "$$" "$LOCK" 2>/dev/null; then LOCK_HELD=1; return 0; fi
  owner=$(readlink "$LOCK" 2>/dev/null) || owner=
  if [ "$owner" = "$$" ]; then LOCK_HELD=1 PULLED=1; return 0; fi
  if lock_stale "$owner"; then
    rm -f "$LOCK"
    if ln -s "$$" "$LOCK" 2>/dev/null; then
      LOCK_HELD=1
      log "lock: reclaimed from pid=$owner"
      return 0
    fi
    owner=$(readlink "$LOCK" 2>/dev/null) || owner=
  fi
  log "lock: busy, held by pid=$owner"
  printf 'update: another update is running (pid %s); if it is not, remove %s\n' "${owner:-?}" "$LOCK" >&2
  exit 75
}

# pull updates the checkout, then runs the pulled script in this process:
# exec keeps $$, so the lock stays ours and tells the new script the pull
# is done.
pull() {
  if [ "$PULL" -eq 0 ] || [ "$PULLED" -eq 1 ]; then return 0; fi
  say "pulling $SRC"
  git -C "$SRC" pull --ff-only || die "git pull failed in $SRC; nothing was built"
  log "pull: ok src=$SRC"
  exec sh "$SRC/tools/update/update.sh" "$@"
}

# build compiles both binaries with one go build into BIN/.rex-stage, on
# the same filesystem as BIN, so installing them is a rename.
build() {
  STAGE=$BIN/.rex-stage
  rm -rf "$STAGE" || die "cannot clear $STAGE"
  mkdir "$STAGE" || die "cannot create $STAGE"
  VER=$(git -C "$SRC" describe --tags --always --dirty 2>/dev/null) || VER=dev
  branch=$(git -C "$SRC" rev-parse --abbrev-ref HEAD 2>/dev/null) || branch=?
  say "building $VER ($branch) from $SRC"
  log "build: src=$SRC branch=$branch version=$VER"
  set -- go -C "$SRC" build -trimpath \
    -ldflags "-X github.com/tristanbietsch/rex/internal/version.Version=$VER" \
    -o "$STAGE/" ./cmd/rex ./cmd/rex-daemon
  if [ "$VERBOSE" -eq 1 ]; then
    "$@" || die "build failed; nothing was installed"
  elif ! "$@" >"$STAGE/build.log" 2>&1; then
    tail -n 20 "$STAGE/build.log" >&2
    die "build failed; nothing was installed"
  fi
  # A build that does not run here (a stray GOOS/GOARCH, a broken
  # toolchain) stops before anything is installed.
  "$STAGE/rex" --version >/dev/null 2>&1 || die "the new rex does not run; nothing was installed"
  "$STAGE/rex-daemon" -version >/dev/null 2>&1 || die "the new rex-daemon does not run; nothing was installed"
}

# install_bins renames each changed binary over the installed one, the
# daemon first. An unchanged binary keeps its file and mtime, so no board
# is told a new build arrived. Signals wait until both renames are done.
install_bins() {
  changed=
  trap '' INT TERM HUP
  for f in rex-daemon rex; do
    if cmp -s "$STAGE/$f" "$BIN/$f"; then continue; fi
    if ! chmod 0755 "$STAGE/$f" || ! mv -f "$STAGE/$f" "$BIN/$f"; then
      set_traps
      die "cannot install $BIN/$f"
    fi
    changed="$changed $f"
  done
  set_traps
  if [ -z "$changed" ]; then
    say "$BIN already has $VER"
    log "install: unchanged version=$VER"
  else
    say "installed$changed ($VER) in $BIN"
    log "install:$changed version=$VER bin=$BIN"
  fi
}

# switch moves the running daemon and boards onto the installed build.
# rex upgrade decides what is stale; -no-cold means a session (this one,
# maybe) is never killed. Ctrl-C waits: a restart cannot stop halfway.
switch() {
  if [ "$RESTART" -eq 0 ]; then
    say "left the daemon alone (--no-restart); run rex upgrade to switch it"
    return 0
  fi
  rc=0
  trap '' INT
  "$BIN/rex" upgrade -o text -no-cold -if-stale 2>"$STAGE/upgrade.err" || rc=$?
  set_traps
  case $rc in
    0)
      cat "$STAGE/upgrade.err" >&2
      log "upgrade: ok"
      ;;
    4)
      say "the daemon is not running; the next rex starts the new build"
      log "upgrade: daemon not running"
      ;;
    *)
      cat "$STAGE/upgrade.err" >&2
      log "upgrade: exit $rc"
      printf 'update: installed, but the daemon is not on the new build (rex upgrade exit %s); re-run, or follow the hint above\n' "$rc" >&2
      exit 3
      ;;
  esac
}

# shadow warns when $PATH would not run the rex just installed.
shadow() {
  found=$(command -v rex 2>/dev/null) || found=
  if [ -z "$found" ]; then
    warn "$BIN is not on \$PATH"
  elif [ ! "$found" -ef "$BIN/rex" ]; then
    warn "\$PATH runs $found first, not $BIN/rex"
  fi
}

main() {
  parse "$@"
  resolve_paths
  set_traps
  lock
  pull "$@"
  build
  install_bins
  switch
  shadow
}

# One line: sh reads it whole before running it, so an edit to this file
# while it runs cannot change what runs.
main "$@"; exit
