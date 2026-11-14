# ON1 `rex onboard`: guided tour and agent detection

**Summary**
- New command `rex onboard`: a step-by-step terminal tour for a new user. It
  checks the install, finds which coding agents are installed, runs
  `rex setup` when there is no config, then teaches the board keys.
- Agent detection runs in the background while the tour starts. Missing
  agents are listed with their install hint. Zero installed agents is an
  error.
- The new-agent page offers only installed agents. An agent installed while
  the daemon runs shows up on its own within seconds, with no `rex reload`.
- README gets the command in Install, Start in 60 seconds and Common tasks.

**Current behavior**
- `rex setup` (`cli/setup/setup.go`) is a config wizard: default tool, model,
  effort, shell block, auto-trust. It teaches nothing about the board.
- The daemon decides which tools are installed with `exec.LookPath` on the
  first word of each tool's `command` (`registry/installed.go`). It probes
  only when the registry is set: at boot, on `rex reload`/SIGHUP and on a
  settings change (`daemon/boot/catalog.go` `Reload`, `Refresh`). An agent
  installed later stays hidden until one of those happens.
- The TUI already filters: `pickerTools` (`tui/tool_pickers.go`) offers tools
  the daemon reports installed and puts the rest in Settings → Onboarding
  with `install_hint`. The new-agent page links there (`newagent_agent.go`).
- `rex doctor` hardcodes the tool list (`doctor.go` `checkToolBinaries`:
  `claude codex agy ollama qwen cursor-agent grok`) and reports each as INFO,
  so "no agent at all" never fails a check.
- The daemon's PATH is the PATH of the shell that first started it. A binary
  installed into a directory added to PATH later is invisible to the daemon
  even after a probe.

**Design**

*Command*
- `rex onboard [-step N] [-check] [-socket path]`. Lifecycle section, class
  interactive. Short: "guided tour for new users: checks, agents, keys".
- Needs text format and a terminal (stdin and stdout TTY). Otherwise exit 5
  `needs_terminal`, hint `rex onboard -check`.
- `-check` runs steps 1 and 2 only, no prompts. Class query, all output
  formats. Exit 1 when a check fails. For scripts and agents.
- `-step` and `-check` together: exit 5.
- `-step N` starts at step N (1-5). Each step prints `[N/5] Title`. `enter`
  continues, `b` goes back, `q` quits. Quitting prints
  `resume with: rex onboard -step N`.

*Steps*
1. **Check install.** Reuse doctor checks: rex binary, daemon binary, daemon
   reachable (start it if not, as the board does), config load. A FAIL stops
   the tour with the doctor message and `rex doctor -verbose`.
2. **Agents.** One row per known agent from the registry (`visibleTools`,
   so `echo` and the `gemini` alias stay out):
   - `✓ Claude Code   claude   /path/to/claude`
   - `· Codex  codex    not installed: see https://github.com/openai/codex`
   - extra agents (qwen, cursor, grok) installed while `extra_agents` is off:
     `✓ … installed, hidden: rex config set extra_agents true`.
   Errors and warnings:
   - zero installed: FAIL `no_agents`, "rex needs at least one coding agent",
     the hint list. Tour waits on `r` (rescan) or `q`.
   - found by the CLI but not by the daemon: WARN `daemon_path_stale`, "the
     daemon cannot see <bin>; it started with an older PATH", hint
     `rex daemon restart -cold`. A hot restart re-execs with the old
     environment and does not help. `-cold` already asks before it kills
     running sessions; onboard only prints the hint, never runs it.
   - `agy` below the tested version: existing doctor WARN.
   `r` rescans: the CLI probes and asks the daemon to rescan (below).
3. **Configure.** No config, or an empty one: offer `rex setup` (runs the
   wizard in place, then returns to the tour). Config present: show the
   default tool, model and effort. Default tool not installed: WARN and offer
   `rex config set default_spawn_tool <installed id>` (lists installed ids).
4. **Board tour.** Four short pages, text only, no live session: open the
   board (`rex`); sections READY / WAITING / WORKING / DONE; `n` new agent
   (only installed agents are listed), `enter` attach, `qq` detach; `cc` done,
   `uc` undo, `tab` next needs-you, `?` keys, `S` settings. Key names come
   from the TUI keymap, not literals, so the tour cannot drift from `?`.
5. **Next steps.** `rex` to start, `docs/quickstart.md` for a test agent that
   needs no API key, `rex doctor`, `rex onboard -check`. Writes the onboarded
   marker.

*Background detection*
- `rex onboard` starts the agent probe in a goroutine before step 1 prints,
  so step 2 is usually ready on arrival. Two views, merged in step 2:
  - CLI view: `registry.InstalledMap` on the merged registry (builtin plus
    `tools.yaml`), with the CLI's PATH. Needs no daemon.
  - Daemon view: `ToolInfos` after a `RescanTools`. Arrives once step 1 has
    the daemon up; until then step 2 shows `checking…` for that column.
  The daemon view decides what the new-agent page offers; the CLI view only
  feeds the stale-PATH warning.
- Daemon: new `Server.RescanInstalled() bool`. Re-runs `InstalledMap` on the
  current registry, and only when the result differs stores new
  `ToolInfos` and sends `ToolsChanged`. Models and the registry stay as they
  are (no model detection, no reload).
- The daemon calls it every 30 s (`catalog.go`, stopped on shutdown) and on a
  new `RescanTools` intent. The TUI sends `RescanTools` when the new-agent
  page opens; `rex onboard` sends it on `r`.
- Cost: one `stat` per PATH entry per tool, about 10 tools. No subprocess.

*New-agent page*
- Already lists only installed agents. Keep it. Rows appear and disappear on
  `ToolsChanged` (`refreshNewAgent` exists).
- Footer row `N more agents available` opens Settings → Onboarding (exists).
  No change beyond the copy if needed.

*Doctor*
- `checkToolBinaries` reads the registry (`visibleTools`) instead of the
  hardcoded list.
- New check `agents installed`: FAIL with zero installed. Rows stay INFO.

*First-run nudge*
- Bare `rex` with no config file and no onboarded marker opens the board
  with the notice `new to rex? quit (q) and run rex onboard for a guided
  tour`. Agent mode never opens the board, so never sees it.
- Marker: `onboarded` file in `paths.DataDir()` (`~/.local/share/rex`),
  written at step 5. An existing user with a config never sees the nudge.

*Rules*
- rex never installs an agent. Install hints are text (registry rule). No
  `curl | sh` in any hint.
- No new setting. No new persisted state besides the marker.

**Files**
- `internal/surface/cli/setup/onboard.go` (new), `onboard_test.go`,
  `testdata/onboard_*.golden`.
- `internal/surface/cli/core/commands.go`: table entry.
- `cmd/rex/main.go`: `case "onboard"`; first-run nudge on bare `rex`.
- `internal/surface/cli/setup/doctor.go`: registry-driven tool rows, new check.
- `internal/daemon/server/server.go`: `RescanInstalled`.
- `internal/daemon/boot/catalog.go`: 30 s ticker.
- `internal/wire/protocol`: `RescanTools` intent; `docs/protocol.md`.
- `internal/surface/tui/newagent*.go`: send `RescanTools` on open.
- `README.md`, `docs/cli.md`, `docs/quickstart.md`, `CHANGELOG.md`
  `[Unreleased]`, `tools/coverage/baseline.txt`.

**Tests**
- Probe: temp dir PATH with stub binaries (`.cmd` on Windows), 0 / 2 / all
  installed. Zero gives `no_agents` and exit 1 under `-check`.
- Stale daemon PATH: CLI stub present, fake daemon reports not installed,
  gives `daemon_path_stale`.
- `RescanInstalled`: add a stub mid-test, one `ToolsChanged`; no change, no
  event.
- Tour: scripted stdin (`enter`, `b`, `q`), golden output per step;
  `-step 4` starts at 4; non-TTY exits 6.
- Doctor golden updated; `TestRun_EveryCommandIsKnown` passes.
- Tour keys: test that every key the tour names is bound in the keymap.
- `make check` green.

**Decisions** (2026-11-13)
1. New-agent page keeps the one `N more available` row linking to install
   hints.
2. Rescan every 30 s, plus on page open and `r`.
3. The tour outside a terminal exits 5, like `rex setup` without `-yes`
   (the agent card groups interactive commands at exit 5).
4. The first-run hint is a board notice (`RunOptions.Notice`), not a
   stderr line: the board's alt screen would hide a line printed before it.
