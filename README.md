# rex

Rex is a terminal kanban for parallel AI coding-agent sessions. Two binaries: `rex` (CLI and TUI) and `rex-daemon` (PTY supervisor). You run many agent CLIs at once; Rex tracks state, transcripts, and input prompts.

Requires Go 1.26+ to build from source. Targets macOS and Linux.

## Install

From a checkout:

```sh
./install.sh
export PATH="$HOME/.local/bin:$PATH"
rex setup
```

Or:

```sh
make install PREFIX=$HOME/.local
export PATH="$HOME/.local/bin:$PATH"
rex setup
```

Or without a clone:

```sh
go install github.com/tristanbietsch/rex/cmd/rex@latest
go install github.com/tristanbietsch/rex/cmd/rex-daemon@latest
```

Install both binaries. Put the install directory on `PATH`.

## Use

```sh
rex daemon start
rex new "hello rex" --tool echo --model short --slug demo
rex ls
rex status
```

`rex ls` prints a session table. `rex status` prints one summary line. Run `rex` with no arguments for the board.

## Next

- [docs/index.md](docs/index.md) — documentation map
- [docs/quickstart.md](docs/quickstart.md) — install through first session
- [docs/cli.md](docs/cli.md) — command reference
- [docs/architecture.md](docs/architecture.md) — processes and data flow
- [docs/agents/](docs/agents/) — guide for contributors and coding agents

Go source wins when documentation disagrees with behavior.
