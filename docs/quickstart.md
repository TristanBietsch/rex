# Quickstart

Goal: install Rex, spawn one test session, and read its status from the shell.

End state: `rex-daemon` running, one `echo` session on the board, and a `rex status` line you can parse.

Requires: macOS, Linux, or Windows 10 1809+ / 11; Go 1.26.10+ if you build from source (the installers check); network not required after build.

## 1. Build and install

From a repository checkout:

```sh
./install.sh
export PATH="$HOME/.local/bin:$PATH"
```

Or build in place:

```sh
make build
export PATH="$PWD:$PATH"
```

On Windows (PowerShell), `install.ps1` installs to `~\.local\bin` and adds it to your user PATH; open a new terminal afterwards. The `echo` test tool used below needs `bash` on PATH (Git for Windows provides it).

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
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
