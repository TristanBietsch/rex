# Quickstart

Goal: install Rex, spawn one test session, and read its status from the shell.

End state: `rex-daemon` running, one `echo` session on the board, and a `rex status` line you can parse.

Requires: macOS, Linux or Windows 10 1809+. Go (the version in `go.mod`) only if you build from source. Network not required after install.

## 1. Install

The latest release, checked against its `SHA256SUMS`:

```sh
curl -fsSL https://raw.githubusercontent.com/TristanBietsch/rex/master/install.sh -o install.sh
sh install.sh
export PATH="$HOME/.local/bin:$PATH"
```

On Windows (PowerShell):

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/TristanBietsch/rex/master/install.ps1 -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

It installs into `~\.local\bin` and prints the command that adds it to
your PATH if needed (it never edits PATH itself). Upgrade later with
`rex update`.

From a repository checkout:

```sh
./install.sh --from-source
export PATH="$HOME/.local/bin:$PATH"
```

Or build in place:

```sh
make build
export PATH="$PWD:$PATH"
```

## 2. First-time config

```sh
rex setup --yes --force
```

Writes `~/.config/rex/config.yaml` if missing. Skips the interactive wizard.

## 3. Start the daemon

```sh
rex daemon start
```

Example output:

```text
rex-daemon started (pid 10222)
```

The TUI starts the daemon automatically; this step makes the quickstart explicit.

## 4. Spawn a session

```sh
rex new "hello rex" --tool echo --model short --slug quickstart-demo
```

Example output (short id and slug vary):

```text
17af	quickstart-demo
```

The `echo` tool is built in. It needs no API keys.

## 5. List and status

```sh
rex ls
```

Example output:

```text
ID     STATE         TOOL       MODEL                 SLUG                      LAST EVENT
17af   working       echo       short                 quickstart-demo           0s ago
```

```sh
rex status
```

Example output:

```text
0 awaiting input · 1 working · 0 completed
```

Exit status is 0 while no session needs input. Exit status is 1 when any session is `needs_input`.

## 6. Open the board

```sh
rex
```

Press `q` and confirm, or `:bg`, to leave the TUI without stopping sessions.

## Next

- [cli.md](cli.md) — full command reference
- [tui.md](tui.md) — keys and focus modes
- [howto/headless-ci.md](howto/headless-ci.md) — script without the TUI
- [architecture.md](architecture.md) — how the pieces connect
