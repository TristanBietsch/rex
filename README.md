# ∴ rex 

Rex is a terminal kanban for parallel AI coding-agent sessions. Two binaries: `rex` (CLI and TUI) and `rex-daemon` (PTY supervisor). You run many agent CLIs at once; Rex tracks state, transcripts, and input prompts.

Requires Go 1.26.11+ to build from source. Targets macOS, Linux, and Windows 10 1809+ / 11.

## Install

macOS or Linux, latest release (no Go needed). Download the installer, read
it, then run it:

```sh
curl -fsSL https://raw.githubusercontent.com/TristanBietsch/rex/master/install.sh -o install.sh
sh install.sh
export PATH="$HOME/.local/bin:$PATH"   # if the installer says it is not on PATH
rex setup
```

It downloads the latest [release](https://github.com/TristanBietsch/rex/releases)
for your OS and arch, checks it against the release's `SHA256SUMS`, and
installs `rex` and `rex-daemon` into `~/.local/bin` (`PREFIX=/some/dir` for
another place). No sudo, and it never edits your shell profile. Pin a version
with `REX_VERSION=v2.0.0 sh install.sh`. On Linux, sound needs ALSA
(`libasound2`), which desktop systems already have.

Upgrade with `rex update` (or run the same two lines again). A running
daemon keeps the old binary until you switch it; both print the command.

Or download an archive from the release page yourself and check it:

```sh
sha256sum -c SHA256SUMS --ignore-missing   # shasum -a 256 -c on macOS
```

From a checkout (needs the Go version in `go.mod`):

```sh
./install.sh --from-source   # or: make install PREFIX=$HOME/.local
export PATH="$HOME/.local/bin:$PATH"
rex setup
```

On Windows (PowerShell), download the installer, read it, then run it:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/TristanBietsch/rex/master/install.ps1 -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1   # latest release into ~\.local\bin
rex setup
```

It downloads `rex_<version>_windows_amd64.zip` and `SHA256SUMS` from the
latest release (`-Version v2.0.0` or `REX_VERSION` pins one), checks the zip
with `Get-FileHash`, and installs `rex.exe` and `rex-daemon.exe`. It never
edits your PATH or profile; when `~\.local\bin` is not on PATH it prints the
command that adds it. `-FromSource` builds from a checkout instead (needs Go).
Or unzip the archive from the release page yourself.

To upgrade any release install later, run `rex update` (`rex update -check`
only looks).

Agents run on ConPTY. Claude Code's state hooks need Git for Windows (Claude runs hooks in Git Bash), and the `echo` test tool needs `bash` on PATH.

With Go, no clone:

```sh
go install github.com/tristanbietsch/rex/cmd/rex@latest
go install github.com/tristanbietsch/rex/cmd/rex-daemon@latest
```

`@latest` builds the newest commit on `master` (the module path has no
`/v2`, so Go ignores the `v2.x` tags). Install both binaries and put the
install directory on `PATH`. `rex update` upgrades this kind of install.

## Use

```sh
rex daemon start
rex new "hello rex" --tool echo --model short --slug demo
rex ls
rex status
```

`rex ls` prints a session table. `rex status` prints one summary line. Run `rex` with no arguments for the board.

## Next

- [docs/agents/HANDOFF.md](docs/agents/HANDOFF.md) — AI agents: start here (current state, open work)
- [docs/index.md](docs/index.md) — documentation map
- [docs/quickstart.md](docs/quickstart.md) — install through first session
- [docs/cli.md](docs/cli.md) — command reference
- [docs/architecture.md](docs/architecture.md) — processes and data flow
- [docs/agents/](docs/agents/) — guide for contributors and coding agents

Go source wins when documentation disagrees with behavior.
