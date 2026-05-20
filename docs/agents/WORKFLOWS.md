# Workflows

Exact commands as defined in `Makefile` and verified in this repo (May 2026). `make test` currently fails one test in `internal/catalog/registry` (`TestLoad_UserExtends`); all other packages pass.

## Build

```sh
make build
```

Equivalent:

```sh
go build -o rex-daemon ./cmd/rex-daemon
go build -o rex ./cmd/rex
```

Binaries land in the repository root (`rex`, `rex-daemon`). `.gitignore` ignores them.

## Test

```sh
make test
```

Runs `go test ./...`.

## Lint

```sh
make lint
```

Runs `golangci-lint run` (see `.golangci.yml`: errcheck, govet, staticcheck, revive, ineffassign, unused, gofmt, goimports).

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

Flags: `--skip-build`, `--verbose` / `-v`, `--shell-init`, `--migrate`, `-h` / `--help`. Builds same binaries unless `--skip-build`.

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
