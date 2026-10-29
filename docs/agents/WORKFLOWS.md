# Workflows

Exact commands as defined in `Makefile` and verified in this repo (October 2026). All packages pass `make check`.

## Build

```sh
make build
```

Equivalent (`VERSION` defaults to `git describe --tags --always --dirty`; without git the `-X` flag is dropped and `rex --version` falls back to module build info, then `dev`):

```sh
LDFLAGS="-X github.com/tristanbietsch/rex/internal/surface/cli/core.Version=$VERSION"
go build -ldflags "$LDFLAGS" -o rex-daemon ./cmd/rex-daemon
go build -ldflags "$LDFLAGS" -o rex ./cmd/rex
```

Binaries land in the repository root (`rex`, `rex-daemon`). `.gitignore` ignores them.

## Test

```sh
make test
```

Runs `go test ./...`, then `go -C third_party/vt10x test ./...` (the vendored fork is its own module).

## Lint

```sh
make lint
```

Runs golangci-lint v2 (pinned, via `go run`, so it is built with the repo's Go toolchain) with `.golangci.yml`: copyloopvar, errcheck, errorlint, gocritic (`captLocal` off for gopher-lua's `L`), govet, ineffassign, misspell, nilerr, revive (`error-return`, `error-naming` only), staticcheck, unparam, unused, whitespace; gofmt + goimports formatters.

## Pre-release gate

```sh
make check
```

Runs `go vet`, `go test -race ./...`, `make lint`, and govulncheck (pinned). CI (`.github/workflows/ci.yml`) runs on every push to `master` and every pull request: `make build` and `make check` on Linux and macOS; on Windows `go vet`, `go test -race` for the root module and `third_party/vt10x`, `install.ps1`, and a daemon smoke test (`daemon start`, `new`, `wait`, `ls`, `daemon stop`). Jobs time out after 30 minutes. Actions are pinned by commit SHA; Dependabot (`.github/dependabot.yml`) bumps them and `go.mod` weekly.

## Install (local prefix)

```sh
make install
```

Default `PREFIX=$HOME/.local` → installs to `$HOME/.local/bin/rex` and `rex-daemon`.

```sh
make install PREFIX=/usr/local
```

## Uninstall binaries

```sh
make uninstall
```

Removes `$(PREFIX)/bin/rex` and `rex-daemon`.

## Clean build artifacts

```sh
make clean
```

Removes root-level `rex` and `rex-daemon` binaries only.

## Install script

```sh
./install.sh
```

Flags: `--skip-build`, `--verbose` / `-v`, `--shell-init`, `--migrate`, `-h` / `--help`. Builds the same binaries as `make build` (same version ldflags) unless `--skip-build`, after checking `go env GOVERSION` against the `go` line in `go.mod`. `--shell-init` prints a PATH + completion block for `$SHELL` (zsh, bash, or fish).

Windows: `install.ps1` (`-Prefix`, `-SkipBuild`, `-NoPath`) does the same build, version check, and daemon stop, and adds the bin directory to the user PATH.

## Go install (remote)

```sh
go install github.com/tristanbietsch/rex/cmd/rex@latest
go install github.com/tristanbietsch/rex/cmd/rex-daemon@latest
```

`rex update` uses these module paths.

## Run TUI (default)

```sh
rex
```

No arguments → `internal/surface/cli.RunTUI` → Bubble Tea board. Daemon auto-starts via boot splash if socket unreachable.

## Run daemon manually

```sh
rex-daemon
```

Typical flags: `-socket`, `-state-dir`, `-tools`, `-max-concurrent-sessions`, `-version`. See `modules/cmd.md`.

## First-time setup

```sh
rex setup
```

## Diagnostics

```sh
rex doctor
```

## Module tidy

```sh
go mod tidy
```

Not wired in Makefile; use when changing dependencies.
