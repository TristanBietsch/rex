# rex

Terminal kanban for agent sessions. Spawn many coding agents in parallel; rex
shows what each one is doing, which need your input, and lets you jump into any
of them like opening a tab.

## Install

Pick whichever is easiest.

**One-liner from a checkout** — builds and drops binaries in `~/.local/bin`:

```sh
./install.sh
```

**Makefile** — same thing, configurable prefix:

```sh
make install                       # ~/.local/bin
make install PREFIX=/usr/local     # /usr/local/bin (may need sudo)
```

**`go install`** — if you don't want to clone:

```sh
go install github.com/tristanbietsch/rex/cmd/rex@latest
go install github.com/tristanbietsch/rex/cmd/rex-daemon@latest
```

All three install both binaries (`rex` and `rex-daemon`). Make sure the install
directory is on your `PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"   # if you used the defaults
```

To remove:

```sh
make uninstall   # or: rm ~/.local/bin/rex ~/.local/bin/rex-daemon
```

## Run

```sh
rex                # launch the TUI
rex --version
rex status         # one-line summary
rex ls             # plain-text session list
```

See `docs/` for the full spec, protocol, and design.
