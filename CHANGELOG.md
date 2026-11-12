# Changelog

All notable changes to rex are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed (Windows)

- Saving or loading settings while another rex saves `config.yaml` no
  longer fails with "Access is denied" or a sharing violation.
- A rejected request (an oversized line, say) gets its error reply instead
  of a reset connection.
- With no daemon running, commands say "rex-daemon is not running" and how
  to start it, instead of a raw dial error.
- `rex upgrade` keeps the board's place (selection, filter, scroll) across
  the swap; the saved state was refused for its file mode.
- Paths under your home folder show as `~/dev/rex` in the TUI, as in the
  CLI, and `rex trust` saves the suggested folder that way.

## [2.0.0] - 2026-11-12

The first release since v1. Details for every change follow the summary.

### Highlights

- **Green Ready, check-mark Done**: READY is a bright green `⚑`, DONE a
  calmer green `✓`; new state markers and agent icons, all one cell wide.
- **Full-screen settings and help pages** (`S`, `?`) that look like the
  board, with a section list, search and a detail pane
  (`fullscreen_pages`, on).
- **Themes**: a new default with a violet accent, bundled `dracula`,
  `tokyo_night` and `pantone_safe`, an opt-in `auto` theme, and your own
  theme files in `~/.config/rex/themes/` (`rex theme list|show|new`).
- **Accent color**: pick the accent on top of any theme (`accent_color`,
  `:accent`, settings page with a swatch and live preview): a
  Pantone-inspired name or your own `#hex`, `rgb()`, `hsl()`, `cmyk()` or
  `oklch()`, kept readable on dark and light themes.
- **Lua config**: `~/.config/rex/config.lua` sets any setting, and a trusted
  workspace can carry its own `.rex/config.lua` for display settings.
- **Folder trust with guards**: `rex trust`, the wizard's trust line and
  `auto_trust_dirs` answer agents' folder-trust dialogs for you; trust
  changes need a person at a terminal, `skip_permissions` is opt-in and only
  applies in trusted folders.
- **READY and WAITING you can rely on**: no more READY flapping while
  subagents run, after notifications that need nobody, mid compaction or
  after Esc; a Waiting section for sessions busy on a shell command,
  subagent or web request.
- **Row icons**: fixed icon slots on each board row (finished, asks, what
  it waits on, loop turn, subagents, cost), right-aligned next to the age.
- **More agents**: Qwen Code, Cursor CLI and Grok Build, and Antigravity
  (`agy`) in place of the retired Gemini CLI.
- **Workspaces and loops**: `rex here` / `rex ws open` boards for one
  workspace; loop workflows that repeat a task with a turn counter on the
  row.
- **Undo**: turn snapshots with `:undo` / `:redo` (`turn_snapshots`, off by
  default), and `uc` to undo marking a session done.
- **Usage**: a `:usage` page, a header usage meter and `rex usage`.
- **Waiting icon and screensaver settings**: `waiting_spinner`, and the
  screensaver's counts layout, clock and drift (`screensaver_counts`,
  `screensaver_clock`, `screensaver_drift`).
- **Sound**: `master_volume` is 0–50, and audio settings apply live on every
  open board.
- **The hints footer**: the bottom line holds the key hints (Done column
  keys included); `show_help_bar` hides only the hints.
- **Release builds**: prebuilt binaries for macOS, Linux and Windows on the
  GitHub release page, and an installer that fetches and verifies them.
- **Hot reload**: upgrade without killing sessions. `rex upgrade` loads a
  newly installed build: the daemon hot-restarts in place (every agent
  keeps its process, PTY and screen) and every board swaps into the new
  binary. `rex daemon restart` is hot too; `-cold` keeps the old stop and
  start (Windows always restarts cold). `rex reload` reloads every layer,
  `init.lua` included.
- **New-agent page**: `n` opens a full-screen page with the agent, a
  multi-line task, context (large pastes, `@` paths, `ctrl+o` import),
  `$EDITOR` editing and folder trust on one screen.
- **Windows prompt safety**: agents installed as npm `.cmd` shims no longer
  get their task through `cmd.exe`, so `&`, `|` or `%VAR%` in a task can't
  run commands; over-long tasks are pasted instead of failing.
- **Render fixes**: popups stay straight with CJK and emoji text,
  multi-line pastes keep the help line on screen, short terminals keep the
  prompt, and prompts are typed without escape sequences.

### Changed (green Ready, check-mark Done, new icons)

- READY is green: its rows' marker, the section title, the `N ready`
  header count (and `⚑ N` in the short counts), the `? asks` slot, the
  screensaver count and the legend use a bright "go" green in every theme.
  Warnings keep the amber; a paused loop and the attach scroll chip are
  amber now too.
- DONE rows show a green check mark `✓` (was `●`), in a calmer, darker
  green than READY. The two greens are at least ΔE 20 apart, read at 4.5:1
  on each theme, and stay apart under simulated protan, deutan and tritan
  vision; the glyphs differ too, so no color is needed to tell them apart.
  `ansi` uses green for READY and bright green for DONE.
- New state markers: `⚑` ready (was `◆`), `✓` done (`●`), `✗` failed
  (`✕`), `⊘` crashed (`○`); `◌` queued stays. The loop mark is `↻` (was
  `⟳`, which some fonts drew wider than a cell), and the header's short
  counts show working with the spinner's first frame instead of `⟳`. Every
  marker is one cell, never East Asian ambiguous, and drawn by Menlo.
- New agent icons: `claude` `✻`, `codex` `❀`, `antigravity` `✧`, `gemini`
  `⁑`, `ollama` `⌂`, `qwen` `❖`, `cursor` `◮`, `grok` `⍁`, `deepseek` `≋`,
  `kimi` `☾`, `echo` `∿` (rationale in [docs/registry.md](docs/registry.md#agent-icons)).
- `rex ls` and `rex status` use the same markers; with `REX_ASCII=1` done
  is `v` (was `o`).
- The accent clash warning names `ready` instead of `needs`.

### Fixed (sound plays once across boards)

- With several boards open, each session sound (needs you, resume, done,
  new, deleted) played once per board, a few milliseconds apart, which
  sounded like a double or an echo. Boards now claim each event in
  `~/.local/state/rex/sound-claims/` (owner-only, claims last about 10 s)
  and only the first plays it. A workspace board or a muted board never
  claims a sound it would not play, so it can't silence another board.
  `audio_dedupe` (Settings → Audio, on) turns it off. Boards from an older
  build don't take part; restart them.

### Added (accent color)

- `accent_color` (Appearance, applies now; default `theme`, the active
  theme's own accent, so nothing changes until you set it) replaces the
  accent on top of any theme: built-in, bundled, a theme file or a
  `config.lua` theme. The accent paints the prompt arrow, selection marker,
  focused input, modified-setting bullet and command names in the TUI and in
  CLI output.
- Values: `theme`; one of 16 Pantone Color of the Year inspired names
  (`very-peri`, `classic-blue`, `cerulean`, `emerald`, `greenery`,
  `illuminating`, `mimosa`, `tangerine`, `living-coral`, `peach-fuzz`,
  `rose-quartz`, `viva-magenta`, `orchid`, `ultra-violet`, `mocha-mousse`,
  `ultimate-gray`; sRGB approximations, not official Pantone values); or
  `#rrggbb`, `#rgb`, `rgb(r, g, b)` (0–255 or %), `hsl(h, s%, l%)`,
  `cmyk(c, m, y, k)` (naive conversion, approximate) and `oklch(l c h)`.
  Case and spaces do not matter; the value is stored as typed. Anything
  else is refused with the reason by `rex config set`, Lua `rex.set`,
  `:accent` and the settings editor.
- Contrast: a color under 4.5:1 on the theme's background is lightened or
  darkened in OKLCH (hue kept) until it passes, with a notice on the status
  line, in the boot log, in the settings detail pane and in
  `rex config get`/`set`. A color close to a state color (ΔE < 20) gets a
  warning.
- Settings page: the row shows a swatch in the painted color (`##` with
  `ascii_glyphs`, none without color); `←`/`→` cycle `theme` and the names;
  `enter`, `e` or `#` open the editor with a live preview. `e` now opens the
  inline editor on any free-form row `config.lua` does not set.
- `:accent [color]` sets it from the board (no value: the next name).
- A trusted workspace's `.rex/config.lua` may set `accent_color` (display
  only).

### Breaking changes

Each is described in full under **Changed** below.

- `master_volume` is a whole number from 0 to 50 (was 0.0–1.0). Config
  files convert on load, but in `config.lua` a `1` now means 1/50; write
  `50` for full volume (`rex doctor` warns).
- `rex trust <folder>`, and `rex config set`/`reset` or `rex setup
  -auto-trust` of `skip_permissions` and `auto_trust_dirs`, work only for a
  person at a terminal (exit 6 in agent mode, with `-o`, or in a pipe).
- `skip_permissions` is opt-in (default off) and applies only in folders
  under `auto_trust_dirs`.
- `show_help_bar` off hides only the key hints; errors and notices still
  show on the bottom line.
- Glyphs: READY is green with a `⚑` marker (was amber `◆`), DONE is a green
  `✓` (was `●`), failed `✗`, crashed `⊘`, loop `↻`; new agent icons
  (`claude` `✻`, `codex` `❀`, `antigravity` `✧`, `gemini` `⁑`, `ollama` `⌂`,
  `qwen` `❖`, `cursor` `◮`, `grok` `⍁`, `deepseek` `≋`, `kimi` `☾`, `echo`
  `∿`). Themes that set `needs` or `done` colors or glyphs keep them. One space between an icon and its number (`⋔ 3`,
  `▴ 42m`), and row icons in place of the task text, right-aligned next to
  the age (`row_icons` off restores it). No `✓ review` mark; the selected
  row has no background (the `▸` arrow marks it); the `∴ REX` logo is the
  theme's primary text color, not the accent.
- "Needs you" is shown as "Ready"; the number keys follow four sections
  (`1` Ready, `2` Waiting, `3` Working, `4` Done).
- Antigravity CLI (`agy`) replaces Gemini CLI.
- `color_scheme` is now `theme` (the old key is still read).
- CLI: unknown commands exit 5; free text starting with `-` needs `--`;
  `rex rm` without a terminal needs `-force`; declined questions exit 6; an
  ambiguous slug/id selector exits 3; `-h` prints to stdout and exits 0.
- User registry tools that relied on the tool id for prompt delivery must
  set `prompt_args` or `prompt_paste`.
- The daemon refuses an `init.lua` that another user owns or that group or
  others can write.
- `install.sh` installs the latest release by default (`--from-source`
  builds the checkout) and no longer stops a running daemon.
- `rex --version` prints the release tag (`v2.0.0`); builds without one
  print the Go module version or `dev`.
- `n` and `:new` open the full-screen new-agent page (`new_agent_page`,
  on); turn the setting off to get the popup wizard back.
- `rex daemon restart` is a hot restart (sessions keep running); `-cold`
  gives the old stop and start, which asks first when it would kill
  sessions (`-yes` without a terminal, else exit 6).
- Boards swap themselves into a newly installed build after a minute idle
  on the board (`auto_swap`, on); set it off to swap only with `:swap` or
  `rex upgrade`.
- `rex reload` (and `:reload`, `SIGHUP`) also reloads `init.lua`, and
  `rex reload` exits 1 when a layer failed to load.
- Windows: an agent installed as an npm `.cmd` shim is started through the
  program the shim runs, not `cmd.exe`. A shim rex cannot resolve (or a
  `command` that is `cmd.exe`) gets its task pasted, and the launch is
  refused unless every other argument is plain.
- The daemon refuses an `initial_prompt` over 96 KiB (`bad_intent`) before
  spawning anything.

### Added (release builds)

- Release archives and `SHA256SUMS` carry GitHub build provenance
  attestations; check one with `gh attestation verify <file> --repo
  TristanBietsch/rex`.
- Tagged releases publish `rex_<version>_<os>_<arch>` archives for macOS
  and Linux (amd64, arm64) and Windows (amd64), each with `rex` and
  `rex-daemon`, plus `SHA256SUMS` (`.github/workflows/release.yml`,
  `tools/release/build.sh`, `make dist`).
- `install.sh` downloads the latest release (or `REX_VERSION`), verifies it
  against `SHA256SUMS`, and installs into `~/.local/bin` without sudo or
  shell profile edits. `--from-source` builds a checkout (or a fresh clone).
- `rex update` updates a release install (`install.sh`, `install.ps1`, an
  unpacked archive) from GitHub releases: it downloads the archive for your
  platform over https, checks it against `SHA256SUMS`, extracts only `rex`
  and `rex-daemon`, and swaps them in atomically. The daemon keeps running;
  the result names the next step (`rex upgrade` when the new build has it,
  else `rex daemon restart`, which ends sessions). `-version vX.Y.Z` or
  `REX_VERSION` pins a release; a downgrade, a reinstall and a `dev` build
  need `-force` (a `go install` build refuses `-version`). Homebrew,
  MacPorts, Scoop, Nix, Flatpak and distro installs are refused with
  their own upgrade command. `rex update -check` looks up the latest
  release and prints `status` (`up-to-date`, `update-available`,
  `running-newer` or `unknown`); `GITHUB_TOKEN` is sent to the GitHub API
  when set.
- `install.ps1` installs the latest Windows release (or `-Version` /
  `REX_VERSION`), verified with `Get-FileHash` against `SHA256SUMS`, into
  `~\.local\bin`. It prints PATH advice instead of editing your PATH, and
  leaves a running daemon alone. `-FromSource` keeps the old build from a
  checkout.

### Fixed (help for command groups)

- `rex help daemon` printed `daemon: unknown subcommand "-h"`. `rex help
  <cmd>`, `rex <cmd> -h` and `rex <cmd> --help` now print help for every
  command, including `daemon`, `config`, `theme` and `ws`, and `rex help
  render` works although `render` uses `-h` for its height.

### Changed (docs)

- README rewritten: what rex is first, then install, a 60-second start, how
  it works, concepts, common tasks, configuration, security, upgrading from
  v1 and a map of every doc. The quickstart's `rex ls` example, the board
  header in `docs/tui.md` and the glossary's board entry match v2.

### Changed (install and version)

- `rex --version`, `rex-daemon -version`, the help page and `rex doctor`
  show the release tag set at build time. `go install` builds show the Go
  module version; other builds show `dev`.
- `install.sh` leaves a running daemon on the old binary instead of
  stopping it (which ended its sessions) and prints how to switch.
- `rex update -source` runs `install.sh --from-source` (`install.ps1
  -FromSource` on Windows).
- `install.ps1` without flags installs the latest release; building from
  the checkout needs `-FromSource`.

### Changed (board polish)

- Board rows no longer paint a background on the selected row (the patchy
  bar behind its icon cells). The `▸` arrow marks the selection.
- The `∴ REX` logo (board header, settings and help page headers,
  screensaver) is drawn in the theme's primary text color instead of the
  accent: white on dark themes, dark on light ones.
- The row icon strip is right-aligned: it ends next to the age, and the
  name takes the room on the left, so long names are no longer cut while
  there is space.
- A Ready row whose turn simply finished no longer shows `✓ review`; the
  why slot shows only `? asks` and the waiting reasons. The legend drops
  the `✓ review` row.

### Changed (tests)

- Tests: hot restart (R20) refusals and rollbacks are covered: restart mode
  validation, a refused preflight, a session that starts during the freeze,
  a failed meta write, handoff file or fd inheritance, and the restarting
  flag refusing spawn, resume, delete and complete of frozen sessions.
  Handoff file validation errors, Boards/SwapBoards replies and partial
  Reloaded reports are covered too. Coverage floors: `internal/daemon/server`
  92 (was 91, which `make check` missed at random), new
  `internal/daemon/handoff` 86.

### Fixed (Windows prompt delivery)

- Windows: an agent installed as an npm `.cmd` shim (`claude.cmd`,
  `qwen.cmd`) got its task through `cmd.exe`, so `&`, `|`, `%VAR%`, `^`,
  quotes or a line break in a task could run commands. The daemon now
  starts the program the shim runs (`node.exe` and the script, or the
  package's `.exe`) directly. A shim it cannot resolve, or a `command`
  that is `cmd.exe`, gets the task pasted into the agent instead and starts
  only when every argument (folder paths and JSON included) is plain; else
  the launch is refused with an error naming the shim and the argument.
- Windows: a task that would make the command line longer than Windows
  allows (32,767 characters, 8,191 through `cmd.exe`; rex stops at 32,000
  and 8,000) is pasted instead of failing the launch.

### Added (new-agent page)

- `n` and `:new` open a full-screen new-agent page instead of the popup
  wizard: the agent, a multi-line task, context and folder trust on one
  screen, with a step rail (Agent, Task, Context, Trust, each with a mark
  and a word) that becomes a one-line strip under 60 columns. It opens in
  the task editor with the agent you launched last. `enter` launches from
  the editor, `ctrl+s` from anywhere, `ctrl+j` / `alt+enter` add a line,
  `ctrl+e` edits the task in `$EDITOR`, `ctrl+g` opens the Agent screen
  (agents, models with a `/` filter, effort, skills), `ctrl+t` / `ctrl+a`
  trust the folder as in the wizard, and `?` outside the editor shows the
  page's keys. Setting `new_agent_page` (Features, on) turns it off to get
  the wizard back; turning it off while the page is open keeps the draft.
- Context: a paste of more than 8 lines or 1 KiB becomes a `paste N` item
  with a `[paste N · L lines]` placeholder in the task (a paste never
  launches); `@` in the task completes workspace paths; `ctrl+o` opens an
  import picker over the workspace (`git ls-files` in a repo, so
  gitignored files stay out; at most 10,000 entries or 300 ms). Items can
  be previewed (`enter`), renamed (`r`), removed (`x`) and, for pastes,
  edited in `$EDITOR`. The agent gets one first prompt: the task, then
  `Context from rex (read before starting):` with a `- file:` or `- dir:`
  line per reference and each paste in a `<pasted …>` block. Files are
  referenced by workspace-relative path, never copied. A draft whose files
  were added in another folder says so (`!`) and takes no new paths until
  they are removed.
- Safety: a path must resolve, through every symlink, inside the session
  folder and be a regular file or folder (FIFOs, sockets and devices are
  refused unopened); names that look like secrets (`.env`, `*.pem`,
  `id_rsa*`, …) need a `y`. Pasted and typed text loses escape sequences,
  control, bidi, zero-width, tag and variation-selector characters and the
  invisible math operators, and `</pasted>` inside a paste is escaped. Caps: 32 KiB per paste, 64 KiB of pasted text, 32
  items, a 96 KiB prompt (16 KiB for agents that take a pasted prompt);
  the last row shows the prompt's size against the cap. Ollama, which
  takes its prompt as typed lines, only launches a one-line prompt with no
  context.
- Launch checks every cap and path again, then the page stays open with
  `⠋ starting…` until the daemon answers. A failure shows
  `✗ launch failed: … Your draft is kept.`; `esc` keeps the draft in
  memory for the next `n` (nothing is written to disk).
- A board swap (`:swap`, `rex upgrade`) waits while the page is open and
  never writes the draft to its swap file: `auto_swap` waits while a draft
  is kept, and an explicit swap over one drops it with a notice.
- `NewSession` takes an optional `context_summary` (kind, name and size of
  each item, no content); older daemons ignore it.

### Changed (daemon)

- The daemon refuses a `NewSession` whose `initial_prompt` is over 96 KiB
  with `bad_intent` before anything is spawned, instead of a failed exec.

### Added (hot reload, board swap, daemon hot restart)

- `rex daemon restart` is now a hot restart: the daemon re-execs itself in
  place (same PID) and every running agent keeps its process, PTY, screen
  and output. Before the exec the new binary must pass a preflight
  (`rex-daemon -handoff-check`); a binary that fails it, or a failed exec,
  leaves the old daemon running and the sessions untouched. `-cold` keeps
  the old stop and start, and asks first when it would kill running
  sessions (`-yes` without a terminal, exit 6). Not on Windows (cold
  restart with the same question).
- `rex upgrade`: after installing a new build, hot-restart the daemon,
  then have every board swap into the new binary; reports each process.
- `:swap` re-execs the board into a newly installed build keeping
  selection, scroll, filter, search, the open page and the attach popup.
  The board notices a new build within 10 s; with `auto_swap` (new, on by
  default) it swaps by itself after a minute idle on the board.
- An attach popup re-attaches after a daemon hot restart instead of
  showing the session as ended.
- `rex reload`, `:reload`, `SIGHUP` and the Settings page's new `R` also
  reload `init.lua` (a broken one keeps the previous handlers) and report
  what changed, what needs a daemon restart, and each failed layer as
  `file:line: message`. `rex reload` exits 1 when a layer failed.

### Fixed (settings that said they apply live)

- `mouse_enabled` changed in `config.yaml` (or by `rex config set`) now
  turns mouse capture on or off at once, not at the next attach.
- `retire_after_hours` now changes the daemon's default linger time
  without a restart.
- `max_concurrent_sessions` edited in `config.yaml` is pushed to the
  daemon on the board's next reload, as documented.

### Added (screensaver settings)

- `screensaver_counts` (Settings → Behavior, "Screensaver counts"):
  `horizontal` (default, one line as before), `vertical` (one count per
  line under the logo, numbers in one column; back to one line when the
  terminal is too short) or `off` (logo only).
- `screensaver_clock`: `off` (default), `24h` or `12h` shows the time under
  the logo, updated on the minute.
- `screensaver_drift`: on by default; off keeps the screensaver centered
  without turning on `reduce_motion` everywhere.
- All three apply live and can be set from `config.lua` and from a trusted
  workspace's `.rex/config.lua` (display only).

### Added (row icons)

- Board rows show a strip of fixed icon slots after the name in place of
  the task text: `✦` for a row that finished since you last looked at it,
  why it needs you (`? asks`, `✓ review`) or what it waits on (`sh 42s`,
  `web 9s`, `sub 3m`), the loop turn (`⟳ 3/8`, with `loops` on), subagents
  (`⋔ 3`), what a Claude session has cost so far (`$0.42`, with `usage`
  on) and the long-run badge. Slots keep their place, so nothing shifts.
  The preview line under the board shows the selected row's question,
  summary or task. On a phone-width board line 1 gets `✦` and `?` / `✓`;
  line 2 keeps the text. The help legend lists the new marks.
- Settings: `row_icons` (Features, on) turns the strip on; off restores the
  earlier board with the text column. `row_icon_set` picks the slots and
  `ascii_glyphs` draws them in plain ASCII (`* + @ Y ^`).
- Sessions carry `cost_usd` on the wire (Claude Code, from the usage hub;
  sent when it moved by a cent, at most every 2 s; kept across restarts).

### Fixed (render and cleanup port)

- Attach popup rows with CJK or emoji text stay exactly the popup's width,
  so the popup no longer shifts and clicks land on the right cells.
- Popups drawn over wide board text keep straight borders, and a popup
  wider than the terminal is clipped instead of wrapping.
- Pasting multi-line text into the command bar or the `λ` prompt no longer
  pushes the help line off the screen. The `λ` prompt and the wizard's task
  keep the pasted line breaks for the agent; escapes are dropped.
- On a short terminal the board gives up rows; the prompt and help line
  stay on screen.
- The done flash is visible again.
- A quick-spawned session is selected even when another session goes away
  before it starts.
- Detaching from a busy agent no longer leaks a reader; re-attaching to
  the same session can't be marked ended by the old popup.
- `:stats` closes on `esc` only; typing `:stats` over it no longer runs
  board keys. The setup wizard's model field edits at its end, and its
  daemon hint says `rex daemon start`. A daemon that fails to start points
  at `daemon.log` unless `rex-daemon` is missing.
- `rex new` reports the session it spawned when two spawns race.
  `rex fleet set` and `unset` report a fleet the daemon rejects instead of
  printing success.
- `rex uninstall` exits 1 when a removal fails. `rex doctor` and
  `rex uninstall` find `rex-daemon` next to `rex`, as `rex daemon start`
  does. `rex config edit` opens the `lua_config_path` file and accepts an
  `$EDITOR` with arguments (`code -w`).
- The initial prompt and `rex reply` text are typed without escape
  sequences or control characters, so a prompt can't close its own paste.
- The daemon keeps `max_concurrent_sessions` from `config.yaml` across
  restarts, removes subscriptions of closed connections, no longer drops a
  change made while a client's snapshot is taken, replays a cut transcript
  from a clean boundary, removes leftover `meta-*.tmp` files at startup and
  drops bidi controls from `last_line`. Log records carry the process id.

### Fixed (Lua volume warning)

- `rex doctor` warns when `config.lua` (or a trusted workspace's
  `.rex/config.lua`) sets `master_volume` to a number above 0 and at most 1,
  naming the line: `config.lua:2 sets master_volume = 1: the scale is 0–50
  now (50 = max), so this is 1/50, nearly silent; write 50 for full volume`.
  Lua cannot tell `1` from `1.0`, so an old `master_volume = 1.0` (full) now
  plays at 1/50. A fraction (`0.8`) still converts but gets the same nudge.
  `0` (mute on both scales) is not flagged.

### Fixed (audio settings)

- Turning `sound_enabled` on now plays sound right away on a board that
  started muted. Before, a board opened with sound off or `soundset: off`
  never opened the audio device, so turning sound on, picking a soundset or
  changing `master_volume` did nothing until rex was restarted. A soundset
  picked while muted is the one you hear when sound comes back on.
- Audio settings changed with `rex config set`, in `config.yaml` or
  `config.lua`, or on another board, reach every open board within a few
  seconds (`live_reload`). Builds before the live reload applied them only
  on the board where you changed them.

### Added (waiting icon)

- `waiting_spinner` (Settings → Appearance, "Waiting icon") picks the
  Waiting marker, like Spinner type does for working: `clock` (default,
  `◴ ◷ ◶ ◵`, unchanged), `hourglass` (`⧖ ⧑ ⧖ ⧒`, an hourglass turning
  over), `sand` (`⣀`…`⣿`, piling up), `orbit` (`◜ ◝ ◞ ◟`), `pendulum`
  (`⠂ ⡀ ⢀ ⠐`) and `pulse` (`∘ ◦ ⊚ ◦`). One step a second, still under
  `reduce_motion`. Rows, the attach chip, the header counts and the help
  legend follow it. It can be set from `config.lua` and from a trusted
  workspace's `.rex/config.lua` (display only).

### Changed (volume scale)

- `master_volume` is now a whole number from 0 to 50 (default 40, the old
  0.80). The gain is the value / 50. At 50 the settings page and
  `rex config get`/`list` show `max` (txt, agent and JSON output keep 50).
  ◂ ▸ on the settings page step by 1. An existing config converts on load:
  a decimal from 0 to 1 (`0.8`, `1.0`, `0.0`) reads as round(value × 50),
  a whole number (`1`, `40`) as the new scale. In `config.lua` only a
  fraction (`0.8`) is the old scale, since Lua cannot tell `1` from `1.0`.
  `rex config set master_volume NaN` is now refused.

### Added (workspace settings)

- A workspace can carry `.rex/config.lua` (needs `workspaces` and
  `lua_config`): the same Lua as `~/.config/rex/config.lua`, on top of it in
  rex commands run inside the workspace and on its board (`rex here`,
  `rex ws open`), never on the system-wide board or in the daemon. It is not
  read until you run `rex ws trust` at a terminal (it asks, showing the
  file's size and SHA-256; refused for agents and pipes). Trust is stored
  in `~/.local/share/rex/workspace-trust.json`, bound to the real folder,
  the workspace id and the file's bytes: after any edit it is not run until
  trusted again. `rex ws trust -rm` revokes. Even trusted it may set only
  display settings (theme, layout, animation and the like; see
  docs/workspaces.md), never trust, spawn, audit, sound or feature
  switches, and cannot `require`. It is read without following
  symlinks, and must be yours, not group/world-writable, one link, 256 KiB
  at most. A broken one fails the trust settings closed. Its values show
  as `workspace <path>:N` in `rex config list`, `ws` on the settings page,
  and in a new `workspace config.lua` row in `rex doctor`.

### Changed (rex trust needs a terminal)

- `rex trust <folder>` (adding to `auto_trust_dirs`) now works only for a
  person at a terminal, like `rex ws trust`: agent mode, `-o json`/`agent`/
  `txt`/`md` or stdin/stdout not a terminal exit 6 and write nothing.
  Add folders yourself, at a terminal or in the TUI (`ctrl+t`,
  `:trust`). `rex trust -ls` and `-rm` are unchanged.
- The same terminal-only rule now covers every CLI path that changes a
  trust setting (`skip_permissions`, `auto_trust_dirs`): `rex config set`
  and `rex config reset` of those two, and `rex setup -auto-trust` (with
  `-yes` or the wizard; `-preview` is unchanged). They exit 6
  (`needs_terminal`) and write nothing in agent mode, with `-o` or a pipe.
  Other settings, `rex config get`/`list` and `rex setup -yes` without
  `-auto-trust` stay scriptable. Scripts that set these two keys must now
  run at a terminal (or edit `config.yaml` themselves).
- `rex ws trust` shows the workspace's `.rex/config.lua` above its `[y/N]`
  question: the exact bytes it hashes and trusts, each line flattened (no
  escape, control or bidi characters), at most 40 lines with
  `… N more lines`, plus the size and SHA-256 as before.

### Fixed (workspace trust)

- `rex ws trust` and `rex ws trust -rm` run at the same moment no longer
  lose one another's change (a revoke could be undone): both hold a lock
  on `workspace-trust.json` while they rewrite it.

### Fixed (trust saves)

- `rex trust`, `:trust` and the wizard's `ctrl+a` no longer replace the
  `auto_trust_dirs` list in `config.yaml` with just the new folder while
  `config.lua` is broken (the value then reads empty, failed closed); they
  now edit the list `config.yaml` holds.

### Fixed (READY edge cases)

- A Claude Code session whose background subagent finished while the agent
  was still busy no longer shows READY at the end of that turn and then
  WORKING again when the subagent's result arrives: it stays in WAITING
  until the result is handled (at most 10 s if it never comes).
- Notifications that need nobody (sign-in done, an MCP form answered, a
  background session finished, a usage-limit wait resumed, a mod notice,
  and types newer than rex) no longer make a session READY. A permission
  prompt still does, and the tool wait it interrupted shows again after
  the answer.
- An auto-compaction in the middle of a turn no longer shows READY.
- A turn that ends on an API error (rate limit, overload, sign-in) is READY
  at once instead of after 30 s, or never while subagents ran.
- A subagent whose stop report was lost (crash, kill) no longer keeps the
  session in WAITING for 15 min: Claude's list of running background tasks
  at the end of the turn drops it.
- A background subagent started in the turn no longer counts as killed by
  an Esc, and a subagent's nested subagents no longer delay READY.

### Fixed (READY while subagents run)

- A Claude Code session waiting on subagents no longer turns READY, chimes,
  and flips back to working in a loop. While subagents run (also after the
  main turn ended) it stays in WAITING (`waiting_on: subagent`) and turns
  READY once, after the last subagent finishes and the agent stops. READY
  means a question or permission prompt, or a finished turn with nothing
  still running.
- A turn you interrupt with Esc or Ctrl+C (Claude sends no hook for it)
  turns READY about 5 s after the key, also mid shell command or mid
  foreground subagent; a background subagent still running keeps it in
  WAITING. Without the key, the fallback is 30 s of a still screen (was
  15 s), 15 min with a tool call still open, never while a subagent runs.
- The daemon shows a move into READY only after it held for 2 s (a question
  shows at once), and logs every state change with its reason.
- The needs-you chime plays once per turn; a new question in the same turn
  chimes again. Sessions carry a `prompts` count on the wire for this.
- Known gap: a background shell command (`run_in_background`) has no
  completion hook; the session can show READY while it runs (the second
  READY when it finishes is silent).

### Added (workspace board, loop cards)

- `rex here` and `rex ws open [path]` open the board for one workspace
  (needs `workspaces`): only that workspace's sessions, `[<id> <name>]` in
  the header in the workspace color, and the window border in that color
  (on by default on a workspace board unless you set `window_border`). The
  wizard and quick spawn start at the workspace root. Exit 5 with ``no .rex/
  here — run `rex ws init` `` outside a workspace. A daemon that does not tag
  sessions shows every session, with one notice.
- With `workspaces` on the daemon tags each new session with its workspace
  (`workspace_id`, kept across restarts; clients cannot set it) and appends
  to `<workspace>/.rex/sessions.jsonl` at spawn and at the first terminal
  state. `Snapshot.workspace_tagging` says the daemon does this.
- With `loops` on, a loop's session row shows the loop marker, `iter/max`
  and the loop state (`⟳ 3/8 running`, `paused: needs you`, `done`,
  `capped: max iterations`, ...). `p` pauses or resumes, `x` stops (after a
  y/N), `L` or `:loops` lists loops with state, turn, running time over
  `max_minutes` and the last check; `:loop pause|resume|stop [sel]` does the
  same by name. Themes can set the marker with `glyphs: loop:`.
  `LoopSummary` gains `max_minutes` and `elapsed_s`.

### Changed

- The daemon now refuses an `init.lua` (and its folder) that another user
  owns or that group or others can write, the rule `config.lua` and theme
  files follow; a symlink is followed only to a file that passes. The
  refusal is one `lua: refusing init.lua` line in the daemon log, and no
  hook runs. Fix it with `chmod go-w ~/.config/rex/init.lua`.
- The board filter chips (and `t`, chip clicks, `:filter`) come from the
  daemon's tool list: one chip per installed agent, so `qwen`, `cursor` and
  `grok` are filterable too. Opt-in built-ins (`gemini`, `deepseek`, `kimi`)
  and the `echo` test tool get no chip.
- New agent icons, so every built-in has its own: `qwen` `▣` (was Codex's
  `◇`), `cursor` `▲` (was `▶`, an emoji-capable arrow), `grok` `◢` (was
  `⬢`, missing from many terminal fonts), `gemini` `□`, `echo` `◎`.
- TUI settings page: values sit flush right in the list, with the modified
  bullet just before them. A switch's `on` is drawn in the primary text color
  and `off` in the dim one, in every theme (`on` was the accent color).
- TUI: a long DONE column's keys (`4 show` / `4 hide` and `:prune clear`)
  moved from its title line to the end of the bottom help line, with the
  other key hints. They drop on narrow terminals before the board keys do.
- `show_help_bar` now hides only the bottom key hints: errors and notices
  still show on that line while it is off.

- Qwen Code, Cursor CLI and Grok Build are `enabled_by_default: true` again:
  visible in the wizard when installed and `extra_agents` is on. `rex setup`
  offers them under the same rule.
- `color_scheme` is now `theme`. The old key is still read (`theme` wins if a
  file has both) and the next save writes only `theme`. Names are no longer
  limited to the built-ins: any theme file name works.

- TUI: help, settings, stats, failures, overview and the wizard share one
  titled square panel, and the board behind them is dimmed. The attach popup
  gets square corners and keeps the board's colors.
- `prompt_glyph` must be one printable character (1 or 2 cells wide):
  `rex config set prompt_glyph` refuses control characters, spaces and
  longer text. `keybind_preset` (one option) is hidden from the settings page
  but still works in `rex config`.
- New default color scheme with a violet accent (`#B392F0`) for the prompt
  arrow, selection marker, focused inputs and logo, so "the cursor is here"
  no longer looks like "working". The previous look is the `classic` scheme
  (`rex config set theme classic`). `noir` and `paper` are retuned to
  meet contrast targets (`paper`'s amber was below 4.5:1). CLI output
  follows `theme`, so it changes with the TUI.
- TUI: the new-session wizard no longer asks for a name. Effort (or skills)
  leads straight to the task; rename the session on the board, or pass
  `rex new -name`, to set one.
- Registry: how the first prompt reaches an agent is now data
  (`prompt_args`, `prompt_paste`) instead of a switch on the tool id; the
  built-in tools behave as before. `trust_args` still has to contain
  `{cwd_list}`, unless the tool sets the new `trust_args_global: true` (a
  switch that covers the whole run, as qwen's does). A user tool that relied on the id switch for `claude`,
  `codex`, `antigravity`, `gemini` or `ollama` under another id must now set
  `prompt_args` or `prompt_paste`.
- TUI: the screensaver also starts when an overlay, prompt, the new-session
  wizard, the settings page or the attach popup is left open (it used to
  start only over the bare board). Waking returns to what was open, as it
  was; the waking key still does nothing else. A terminal resize counts as
  activity and wakes it, and turning `screensaver_enabled` off while it
  shows hides it within a minute.

- "Needs you" is now "Ready" everywhere it shows: the board section and its
  counts, the screensaver, the attach banner, `rex status`, `rex ls`,
  `rex wait` and the help. The overview's "Ready for review" bucket is now
  "Review" (header line `» 2 to review`). Machine names stay: the section
  id `needs` (`:hide needs`, `z1`), the state `needs_input` and the
  `rex overview -o json` key `needs_you`.
- TUI: board columns have fixed widths by default and change only when the
  terminal crosses a width breakpoint (150 and 110 columns). The subagent,
  badge, tokens, span and resume columns are always reserved, so nothing
  shifts when a row changes. Below 110 columns the detailed view shows the
  same columns as the simple one. `column_layout: fit` (Settings →
  Appearance) brings back content-sized columns.
- TUI: one space between an icon and the number after it, so the board is
  less crowded: the subagent count (`⋔ 3`), the long-running badge
  (`▴ 42m`) and the `glyphs` header style (`◆ 4  ◷ 1  ⟳ 2  ● 7`, `✕ 2`).
  The subagent and badge columns are one cell wider.
- TUI: the number keys follow the screen, which now has four sections: `1`
  Needs you, `2` Waiting, `3` Working, `4` Done. `2` and `3` used to jump to
  Working and Done, and `4` is now the key that folds a long Done column.
  `z1`-`z4` hide and show the same sections, and `:filter` accepts `waiting`
  (`wa`); a bare `w` is now ambiguous. With the `waiting_section` feature off
  the keys are `1` `2` `3` as before.

- Interactive commands refuse a format that cannot show them, with exit 5
  and the query that answers the same question: bare `rex` (any format but
  text), `rex attach` (json, md, agent) and the `rex setup` wizard.
- `rex doctor`, `config`, `setup` (`-preview`, `-yes`), `update`, `uninstall`,
  `daemon` and `reload` and `version` take `-o`, `--agent` and `--no-color`.
  Their CLI colors come from the theme (`color_scheme`), not copied hex
  values; piped `doctor` and `setup -preview` (`txt`) have no banner or
  glyph. `-json` keeps its legacy shape (`doctor`, `config list`).
- `rex update` and `rex uninstall` ask on stderr (they asked on stdout) and
  need `-yes` without a terminal or in a non-text format (exit 6); a
  declined `uninstall` question exits 6 (it was 1). Progress lines of
  `update` go to stderr. `setup -yes` prints its report through the same
  formats; `rex setup` (the wizard) refuses non-text formats with exit 5.
  Suggested commands use single-dash flags (`rex new -tool … -model … --
  "your first task"`).
- `rex digest`, `rex stats` and `rex fleet` take `-o`/`--agent`; `rex log`
  and `rex render` take `-o` for text and txt (md and json exit 5). Piped
  `digest` and `stats` output (`txt`) no longer has the banner and rule:
  label and value lines under `# Section` headings. `-json` keeps its
  shape; `-o json` is the same data in the versioned envelope with integer
  millisecond durations. Digest errors name `-date`/`-since` (single dash).
- The session commands (`new`, `reply`, `send`, `wait`, `rename`, `name`,
  `done`, `undone`, `archive`, `complete`, `rm`, `prune`) take `-o`,
  `--agent` and `--no-color`: `text` prints a `✓` line, `json` the result as
  data, `agent` one `ok cmd=… id=… slug=…` line. `txt` (a pipe) prints what
  each command always printed. Confirmation questions and warnings go to
  stderr.
- A declined `rex prune` question now exits 6 (it was 1), like `rex rm`.
  `prune` and `rm` need `-yes`/`-force` in every format but `text`, and
  whenever stdin or stderr is not a terminal.
- Help is rebuilt in the style of `agy`: `rex --help` and `rex <command> -h`
  start with `Usage:`, use Title-case sections with a colon, align their
  columns and end with one hint, with no banner or rule. They are generated
  from one command table, print to stdout (`-h` used to print on stderr) and
  follow `-o` (`txt` in a pipe, `md`, `json`, `agent`). A flag error prints
  the error and a one-line `Usage:` hint instead of the whole flag list. The
  completion scripts now list the same commands as `--help`.
- TUI: the new-session wizard (`n`, `:new`) has a model step again. It is
  skipped for a tool with one model, and starts on `default_spawn_model` when
  the tool is `default_spawn_tool`. It used to pick the first model silently.
- TUI: the wizard has an optional name step (after effort). `enter` on an
  empty field skips it; the name is checked as you type. With no task, the
  name also gives the session's slug.
- TUI: the wizard has an optional skills step (after the name): a checklist
  of `~/.config/rex/skills` that loads the chosen skills into that session
  only. Hidden for tools that cannot load skills (Antigravity), for an empty
  library, and when `spawn_skills` is off.
- `rex setup` now suggests trusting the project's own folder (the git root
  of where you ran it, or the folder itself when it is not a checkout)
  instead of the folder above it. Still never your home folder or anything
  outside it. The logic moved to `settings.SuggestTrustDir`, with
  `settings.AppendTrustDir` and `settings.RemoveTrustDir` beside it.
- **Breaking:** an unknown command now exits 5 (was 1) and, when a real
  command is within two edits, says `did you mean "ls"?`.
- **Breaking:** `rex reply <sel>` with no text on a terminal exits 5 instead
  of waiting for end-of-file; `rex log -n` with a negative number exits 5
  instead of being accepted.
- `rex new` waits for the daemon with `-wait` (default 5s). On timeout it
  says `daemon did not confirm the spawn within 5s; check \`rex ls\`` and
  exits 1, instead of a raw deadline error.
- `rex setup -y` is a real alias of `-yes` (so `-y=false` works). The no-op
  flags `rex ls -short`, `rex ls -show-archived` and `rex new -no-attach`
  are marked "(deprecated, no effect)" in their usage; they are still
  accepted.

- **Breaking:** a selector that is a session's exact slug and also the short
  id (or id prefix) of a different session, such as a slug `feed` and
  another session whose id starts with `feed`, now exits 3 and lists both.
  It used to pick the slug silently, so `rex rm feed` could delete the wrong
  session. Use the full id.

- **Breaking:** `rex rm` without a terminal on stdin now needs `-force`
  (aliases `-yes`, `-y`) and otherwise exits 6, like `rex prune`. It used
  to delete without asking. Declining the prompt on a terminal also exits 6
  (was 1).

- AI descriptions are more specific and use the session state: working,
  waiting for you (they say what is needed), finished or failed. They never
  start with the agent's name ("Claude is…"), are sentence case, and the
  default length is longer (`summary_detail: medium`, 110 characters).
- TUI: an unknown `:` command now says `(try :help)`. Commands and key
  help come from one table, so `?` can no longer drift from what `:` runs.
- `rex ls` and `rex status` on a terminal use the new human layout (state
  markers, color, width-aware columns). Piped output is unchanged.
- The `skip_permissions` setting now applies only in folders under
  `auto_trust_dirs` (also when a session is resumed). In any other folder
  agents start without `--dangerously-skip-permissions` and the daemon log
  says so.
- `NewSession` for a tool or model that is not in the registry now fails
  with `bad_intent` (it was `spawn_failed`), the same error `Resume` gives.
- Skipping agent permission prompts is now opt-in. Claude Code and
  Antigravity no longer start with `--dangerously-skip-permissions` unless
  you set `skip_permissions: true` (default off; read at each spawn). The
  flag moved from the built-in `command` to a per-tool
  `skip_permissions_args` registry field.
- CLI: flags are now accepted anywhere among the arguments, so
  `rex new task -tool claude`, `rex wait p1 -until done`,
  `rex done p1 -after 72h` and `rex attach p1 -read-only` work as
  documented. `--` ends flag parsing. **Breaking:** free text that starts
  with `-` (in `rex new`, `rex reply`, `rex name`) now needs `--` before
  it, for example `rex name p1 -- -clear`; an unknown `-x` after text exits 5
  with a hint. `-h` on any command prints its usage and exits 0 instead of
  5.
- TUI: a simpler board. Rows show state, name, the task (or the agent's
  question when it is waiting on you) and age; `v` brings back the id,
  model, tokens and AI summary. Columns are NEEDS YOU / WORKING / DONE
  with counts, the header reads `N need you · N working · N done`, and
  the help bar lists five keys (`enter open · i new · cc done · tab next
  · ? more`). `tab` jumps to the next session that needs you. The tool
  filter row only shows when sessions use more than one tool. A Done
  column longer than 5 rows folds into its title (`3` shows it). No
  existing key changed.
- Lint: gosec, errorlint, exhaustive, gocritic, noctx, contextcheck,
  nilerr, bodyclose, copyloopvar and durationcheck are enabled. gosec's
  directory and file permission checks (G301, G302, G306) are enforced:
  golangci's `legacy` exclusion preset, which hid G301 and G302, is no
  longer used. Lint exclusions that matched nothing are removed.
- CI: superseded PR runs are cancelled, every job has a timeout, and the
  Linux job runs the full `make check` gate.
- Coverage floors in `tools/coverage/baseline.txt` are raised to the
  measured coverage. `cmd/rex-daemon`, `internal/daemon/boot` and the test
  helpers now have floors.
- `rex daemon stop`, `status` and `restart` act on whatever daemon answers
  on the socket. A PID is shown for information only, as `pid unknown`
  when it cannot be determined. `restart` no longer sleeps between stop and
  start, and it does not start a second daemon if the first will not stop.

### Added

- TUI: settings (`S`, `:settings`) and help (`?`, `:help`) open as
  full-screen pages that look like the board: the `∴ REX` header with the
  cursor row or the search in it, dividers, a hint row, and no board behind
  them. A section (or help group) list sits on the left and the rows on the
  right; settings keep their detail pane under both, and a search or filter
  spans every section. `tab` switches panes, `1`–`9` and `[` / `]` pick a
  section, `q` closes. Under 60 columns the list becomes a one-line strip.
  Errors, such as a row `config.lua` locks, show on the bottom line, also
  with `show_help_bar` off. `::settings` and `::help` while attached detach
  first, as before. Turn `fullscreen_pages` (Features) off to get the
  floating overlays back.
- Three bundled themes: `dracula`, `tokyo_night` and `pantone_safe` (state
  colors from the colorblind-safe border swatches). They are theme files
  built into rex: a file of your own with the same name replaces one, and
  `extends:` and `rex theme new -from` still take only the six built-ins.
  The theme pickers list each name once.
- `config.lua` (Settings → Features → *Lua config*, on, and inert without the
  file): set settings and themes in `~/.config/rex/config.lua` with
  `rex.set("theme", "noir")` and `rex.theme("mine", { extends = "noir",
  colors = { accent = "#B392F0" } })`. `rex.platform` is the OS name; `require`
  reads only `~/.config/rex/lua/`. The file runs in a restricted Lua state (no
  `os`, `io`, `print`, `load`; 250 ms, size and memory limits) and must be
  owned by you and not writable by others. Its values beat `config.yaml` and
  are never written to it: `rex config set` and `rex config reset` on such a
  key exit 5 and name the line. A broken `config.lua` keeps the last good
  values (the trust settings `auto_trust_dirs` and `skip_permissions` fall back
  to their defaults) and never changes plain command output. A Lua theme beats
  a theme file with the same name. `rex complete` and `rex completion` never
  run it. Turn the feature off with `rex config set lua_config false`: the
  files are then never read. The daemon polls `config.lua` and `lua/` with
  `config.yaml` and follows an edit within about 5 s; a failure is one log line.
  The TUI follows it within about 2 s (and on `:reload`), shows Lua themes
  with the theme files, has a `config.lua` boot row, and reports a failure
  as one status-line notice.
  Settings rows `config.lua` sets are locked: the value is dimmed with a
  muted `lua`, edits are refused with the file and line, and `e` opens the
  file at that line in `$EDITOR`. `:theme`, the quick-spawn cascade and the
  setup wizard leave locked settings alone.
  `rex config list` has a SOURCE column (`default`, `config.yaml`, `lua
  config.lua:12`) and `source`/`set_at` in json and agent mode, as does
  `rex config get`; the `txt` layout is unchanged. `rex config edit -lua`
  opens `config.lua` (from a commented template the first time), `rex config
  lua-export` prints `config.yaml` as a starter `config.lua`, `rex theme
  list` marks Lua themes `lua`, and `rex doctor` has a `config.lua` check
  that names any trust setting it grants, with its line. A refused
  `rex.set` of a trust setting makes that setting read its default.
  `rex setup -yes` refuses a setting `config.lua` sets.
  Guide: `docs/howto/lua-config.md`.
- Opt-in `auto` theme: `rex config set theme auto` (or `:theme auto`, or
  the settings page) picks `default` on a dark terminal and `paper` on a
  light one. The TUI checks `COLORFGBG`, then asks the terminal (OSC 11), once
  at start and only when the theme is `auto`. CLI output never asks and uses
  `default`. `default` stays the default theme.
- Trust workflow (no more answering "yes, I trust this folder" by hand):
  `rex trust [dir]` adds a folder to `auto_trust_dirs` (default: the repo
  folder of the working directory, or the folder itself outside a repo; never
  its parent) and prints the exact folder; `rex trust -ls` lists them and
  `rex trust -rm <dir>` removes one. `rex new -trust` trusts one session's
  folder only. The home folder, folders that hold it and relative paths are
  refused with exit 5 and a reason.
- TUI: the new-session wizard shows a trust line under the task input with
  the exact folders involved: ctrl+t trusts this session only, ctrl+a saves
  the repo folder to `auto_trust_dirs` and trusts the session. `:trust
  [folder]` saves the selected session's repo folder (or the working
  directory's, or the folder you name).
- The daemon log says `trust dialog shown; folder not trusted` once when an
  agent's trust dialog appears in a folder that is not trusted, and `rex
  doctor` warns about `auto_trust_dirs` entries it would ignore (home folder,
  a folder holding it, a relative path) or that do not exist.
- `rex doctor` has `tool:` rows for the extra agents too: `qwen`,
  `cursor-agent` and `grok` (with `-verbose`).
- `Undo` and `Redo` take an optional `expect` (the dry run's `target`); a
  changed target is refused before anything is touched. `rex undo` and the
  TUI dialog send it on confirm.
- TUI: the new-session wizard lists only installed agents and shows the
  daemon's merged model list with a `detected` tag (it follows the daemon's
  `Snapshot.tools` and `ToolsChanged`, so an open wizard updates in place).
  Settings → Onboarding lists the agents that are not on `PATH`, dimmed, with
  an install hint (new `install_hint` registry field); the quick-spawn
  pickers use the same lists. With `extra_agents` off qwen, cursor and grok
  never show.
- TUI: `:undo [sel] [-turns N]` and `:redo [sel]` (feature `turn_snapshots`).
  A confirm dialog built on the daemon's dry run: file counts to restore and
  delete, the "shell side effects are not undone" line, and the other live
  session in the folder with a force option for that case only. It shows the
  run's warnings, and the daemon's refusals as status-line notices.
- TUI: `:usage` page with usage per agent and model, a total row, the quota
  bar with percent and reset time, ranges today/7d/30d (`←`/`→` or `1`/`2`/`3`),
  `r` refresh and `/` filter. Estimates are marked `~`, stale readings are dim.
- TUI: a usage meter at the right end of the header (`usage_meter`: `off`,
  `compact`, `full`; default `compact`). A click opens `:usage`. Both follow
  the `usage` feature switch.
- Loop workflows (Settings → Features → *Loop workflows*, off by default):
  `rex loop start <name>` runs a session in a repeat loop. The daemon
  re-sends your prompt each time the agent finishes a turn, until a check
  command exits 0, a marker line shows up, or `max_iterations` /
  `max_minutes` is reached. Loops are YAML files in `~/.config/rex/loops/`
  (`rex loop new <name>` writes a starter); the check is an argument list,
  never a shell string, and only your own, non-writable, non-symlink files
  are read. Loops run only for agents that report why they wait (Claude
  Code today). A permission or question prompt pauses the loop and rex never
  answers it; typing into the session holds the next prompt; at most 2 loops
  run at once; a daemon restart leaves them paused. Also `rex loop
  ls|pause|resume|stop|log`. New `StartLoop`, `PauseLoop`, `ResumeLoop` and
  `StopLoop` intents, a `Loop` event, `Snapshot.loops` and `loop_id` /
  `loop_iter` on sessions (additive). The board has no loop cards yet. See
  [docs/loops.md](docs/loops.md).


- TUI: an optional thick colored window border (`window_border`, off by
  default; `window_border_color`: `random`, a name or `#rrggbb`). `random`
  picks a Pantone-inspired, colorblind-safe color per launch folder, so a
  project keeps its color. The frame takes 2 columns and 2 rows, and is
  hidden on the splash, the screensaver and small terminals. `:border`
  (`::border` in the attach popup) toggles it or picks a color for one run
  without saving.
- Themes: write your own as YAML files in `~/.config/rex/themes/<name>.yaml`
  (`extends` a built-in, override colors, replace the state marker and
  selection glyphs). Files are validated as untrusted data (64 KiB and 64 file
  caps, strict colors and one-cell glyphs, a linked file must itself be safe,
  no file writable by others); a bad file is skipped with a reason and
  the TUI falls back to `default` with a notice. See [docs/themes.md](docs/themes.md).
- `rex theme list`, `rex theme show <name>` and `rex theme new <name>
  [-from <builtin>] [-force]`, in every output format. `:theme [name]` in the
  TUI sets (or cycles) the theme and saves it.
- Live reload in the TUI: it checks `config.yaml` and the theme files every
  2 s while `live_reload` is on and applies what changed (theme, display,
  audio and other "applies now" settings) without a restart. `:reload`
  does it at once, also with `live_reload` off, and now reloads `config.yaml`
  and themes as well as `tools.yaml`.
- The setup wizard and the boot log (`themes.load` row) know your themes.

- Daemon: scheduled description refresh (`summary_schedule`, Settings →
  Features, on by default). Each live session gets a new AI description 5, 10,
  15, 20 and 25 minutes after its run started, then every 30 minutes, and the
  descriptions from the 6th refresh on are one `summary_detail` level longer
  and say what is finished plus what is happening now. A state change still
  refreshes at once. Screen activity no longer triggers a refresh while the
  switch is on, except for a session with no description yet; turn it off
  for the old behavior.
- Color schemes `high-contrast` (pure black and white, bright state colors)
  and `ansi` (your terminal's own 16 colors, selection in reverse video).
- Help overlay scrolls (`j`/`k`, `pgup`/`pgdn`, `g`/`G`, wheel) instead of
  dropping groups, filters with `/`, and ends with a legend drawn in the
  board's real styles (markers and long-running badges). Keys behind a
  feature switch that is off are listed dimmed with `(off)`.
- Settings page: `/` search, `tab` to jump between sections, a `•` on values
  that differ from the default, and a detail pane with the setting's id,
  default, options and when a change applies (live, next attach, next new
  session, after a daemon restart). `enter` edits `auto_trust_dirs` and
  `prompt_glyph` in place; the value goes through the same check as
  `rex config set`.
- Qwen Code (`qwen`), Cursor CLI (`cursor`, binary `cursor-agent`) and Grok
  Build (`grok`) as built-in agents, shown only when their binary is on
  `PATH`. They stay hidden in the wizard until enabled in Settings →
  Onboarding (the TUI does not yet read Settings → Features → Extra agents or
  your `PATH`); with Extra agents off the daemon refuses to spawn them. The
  old `grok` entry is replaced. Check each agent's own permission settings: grok's and cursor's
  default modes can edit files or run commands without asking, and rex never
  answers their folder-trust dialogs (qwen has a session-only switch, used
  for folders in `auto_trust_dirs`).
- Agents that speak OSC 7501 (qwen, grok, antigravity) report their state
  exactly: rex answers their status query on the terminal and removes `TMUX*`
  and `TERM_PROGRAM*` from their environment. `done` reads as ready for
  review, `blocked` and `error` as waiting for an answer. A signed-out cursor
  or grok shows "not signed in" instead of its screen.
- Model auto-detect (Settings → Features → Model auto-detect, on): the
  daemon asks each installed agent for its own model list, caches it in
  `~/.local/state/rex/models-cache.json` for 24 h and refreshes it at start
  and on `rex reload`. Off: no command runs and only the built-in lists show.
- Registry fields `models_cmd`, `model_arg_template`, `model_effort`,
  `prompt_args`, `prompt_paste`, `login_regex` and `status_env`, and an
  `installed` flag plus the merged model list on each tool of the `Snapshot`
  and a `ToolsChanged` event (both additive). The wizard and Settings pickers use them in a later release.
- `rex config set` / `reset`, the Settings page and `rex setup` tell a running
  daemon to reload when they change `skip_permissions` or `auto_trust_dirs`,
  so revoking either applies at once. Hand edits of `config.yaml` still apply
  within 5 s (or on `rex reload` with `live_reload` off).
- Daemon: while `config.yaml` does not parse, `skip_permissions` and
  `auto_trust_dirs` read as off and empty (fail closed); other settings keep
  their previous values until the file is fixed.
- Daemon: one settings hub replaces the scattered reads of `config.yaml`.
  `rex reload`, `:reload` and `SIGHUP` now reload `config.yaml` as well as
  `tools.yaml`, and the 5 s poll feeds the same hub. Settings read at each
  spawn (`skip_permissions`, `auto_trust_dirs`, `resume`, `spawn_skills`,
  `usage_statusline_tap`) now follow the hub, so they apply within 5 s of an
  edit or at a reload. The `live_reload` feature (Settings → Features, on by
  default) switches the polling off; manual reload still works. See
  [settings](docs/settings.md#which-settings-apply-live).

- `rex ws init|id|ls|rename|color`: workspaces. A folder with a `.rex/`
  (id, name, color) is a workspace; `rex ws init` creates it, registers it in
  `~/.local/share/rex/workspaces.json` and adds `/.rex/` to the repo's
  `.git/info/exclude`. Off by default (Settings -> Features -> `workspaces`).
  rex refuses a symlinked or writable `.rex/`, and `$HOME` or `/` as a
  workspace. See `docs/workspaces.md`.
- `rex audit`: an audit trail. The daemon writes one line per finished
  session (tool, state, times, first line of the prompt) to
  `~/.local/share/rex/audit.jsonl`, or to `<workspace>/.rex/audit.jsonl`, and
  always before a session is cleared. `rex audit` filters it by `--ws`,
  `--all`, `--since`, `--tool` and `--state`. On by default (`audit_trail`);
  `audit_full_prompt` (off) also keeps the whole prompt, which can hold
  secrets. A session is never cleared without its record: if it cannot be
  written the session is kept and retried. `rex ws init` also ignores
  `/.rex/audit.jsonl`; commit only `workspace.json`.
- Agent mode switches on by itself when `CLAUDECODE=1` and stdout is not a
  terminal (a Claude Code Bash tool, including inside rex-spawned
  sessions): one line per record, an `ok` trailer, one error line on
  stderr, no color, no prompts. `REX_AGENT=0` turns it off for a run; the
  `cli_agent_auto` setting (Features, on) turns it off for good. A
  terminal is never affected.
- `rex errors [-n N] [-since 24h] [-clear]` shows the CLI error log in every
  format; `rex doctor` gets a `cli errors` row (failures in the last 24
  hours).
- Private CLI error log: a failed `rex` command leaves one JSONL record in
  `~/.local/state/rex/cli-errors.log` (0600, 1 MiB plus one backup) with the
  command, flag names, exit code and kind, a stable error code and the Go
  types of the error chain. It stores structure only, never error or hint
  text or any argument value. On by default; setting
  `cli_error_log` (Advanced) or `REX_ERROR_LOG=0` turns it off.
- `rex agent-help` (also `rex help agent`, and `rex --help` in agent mode)
  prints a compact usage card for LLM agents: line grammar, exit codes, the
  common commands and the interactive ones to avoid. `-o json` gives it as
  data.
- `rex help <command>` is the same as `rex <command> -h`.
- `rex resume <sel>` respawns an exited session in place (same row, id, slug
  and transcript) with the agent's resume arguments, or unmarks a running
  session marked done. `rex resume -print <sel>` prints only the full resume
  id. It exits 5 with `off: enable in Settings → Features (rex config set
  resume true)` while the `resume` feature is off.
- `rex ls -wide` adds a `RESUME ID` column (full id) while the `resume`
  feature is on.
- `rex ls -wide` adds `STARTED` and `ENDED` columns before `NAME` (agent
  lines get `started=` and `ended=`).
- Setting `timestamps` (Features, on by default): switches off the TUI's
  start→end span column and the `rex ls -wide` time columns. The end time
  is still recorded.
- Turn snapshots and undo, off by default (Settings → Features,
  `turn_snapshots`). The daemon snapshots a session's folder at the start
  and end of every agent turn into a private store under
  `~/.local/share/rex/snapshots` (only folders inside a git repo; your repo
  is never touched), and `rex undo <sel> [-turns N] [-force] [-yes]` puts the
  files back the way they were before the last turn, `rex redo <sel>` brings
  them back. Undo refuses while the session is working or another live
  session shares the folder (unless `-force`), asks first with file counts
  (`-yes` without a terminal), and does not undo shell side effects. The
  agent is not told. Snapshots keep file contents, including untracked
  secrets such as `.env`, so read [docs/snapshots.md](docs/snapshots.md)
  before turning it on. Kept 7 days after a session is removed, at most
  2 GiB per folder. It follows what git ignores in your repo (also for a
  session in a repo subfolder), refuses an undo that would overwrite
  anything no snapshot holds (a nested clone, a big file, an ignored
  folder) and needs git 2.32 or newer. `rex doctor` shows the store size per folder, whether
  `git` is found and warns about untracked `.env*` files. New `Undo` and
  `Redo` intents with an `UndoResult` reply (additive). The TUI `:undo`
  dialog comes later.
- Usage tracking, first part (the `usage` feature, on by default):
  `rex-daemon statusline-tap`, a Claude Code status line that sends plan
  rate-limit, cost and model fields to the daemon and then runs your own
  status line with the same input, so yours looks the same. The
  `usage_statusline_tap` setting (`auto`, `on`, `off`; Settings → Features)
  decides when rex adds it: `auto` only when you already have a status line
  (a status line makes Claude hide its footer hints, which rex never does on
  its own), `on` always, `off` never. `usage` off adds no tap and writes
  nothing. See [docs/settings.md](docs/settings.md) and SECURITY.md.
- Usage ledger: with `usage` on, the daemon keeps
  `~/.local/share/rex/usage.jsonl` (mode 0600, append-only): one line per
  session when it ends or is pruned, and at most every 10 minutes for a live
  one. The latest line per session counts, so nothing is counted twice, and
  the file is trimmed to 90 days when the daemon starts. It holds ids, tool
  and model names, estimated tokens and cost, never prompts or output. See
  [docs/paths.md](docs/paths.md).
- `rex usage [-range today|7d|30d] [-tool ID] [-by tool|model] [-o FORMAT]`:
  tokens, cost and plan quota per agent, per model and in total. Token
  counts are rex's own estimate and show `~`; cost comes from Claude Code's
  status line; quota windows show percent used and time to reset. Exits 5
  while `usage` is off. See [docs/cli.md](docs/cli.md).
- Protocol (additive, `ProtocolVersion` unchanged): `Usage` event and
  `Snapshot.usage` with the live plan quotas, the `UsageQuery` intent
  (replies `UsageResult`: totals per tool, per model and overall over
  `today`, `7d` or `30d`) and the `UsageSample` intent the status-line tap
  sends. An older daemon answers `UsageQuery` with `bad_intent`, and the CLI
  tells you to run `rex daemon restart`. See [docs/protocol.md](docs/protocol.md).
- Antigravity: a question or the trust dialog shows as waiting at once, not
  after 15 s. rex asks agy for its terminal status (`AGY_CLI_OSC7501=on`,
  per session, nothing written to `~/.gemini`) and falls back to the footer
  until it arrives. New registry field `detect.status: osc7501`.
- Antigravity: rex keeps the conversation id agy prints when it quits, so
  `uc` and `:resume` work for agy sessions (`resume` block with
  `--conversation={id}`), and a `trust_prompt` fallback answers agy's trust
  dialog in trusted folders.
- Agents start at the size of the attach popup (TUI quick-spawn and the
  wizard) or of your terminal (`rex new` on a terminal), so the first attach
  needs no resize. `NewSession` gained `cols` and `rows`; without them the
  agent starts at 120x32 as before.
- `rex export <sel> [-o json|yaml|md|txt] [--out FILE] [--force] [--max-frames N] [--from-agent-log]`
  writes one session as a readable document: its metadata plus the
  transcript as clean text, never raw PTY bytes. Full-screen agents are
  replayed through the terminal emulator into screen frames (cut at each
  clear-screen and alternate-screen switch, repeats dropped); `echo` and
  `ollama` are escape-stripped. `json` is the default (`version: 1`), `yaml`
  is the same document. Output goes to stdout or `--out` (mode 0600, refuses
  to replace a file without `--force`; the format follows the extension).
  `--from-agent-log` reads Claude Code's own log for the session's resume id
  (roles and text, one line per tool call) and falls back to frames with a
  notice. A running session is read up to its current end. Nothing is
  uploaded. See [docs/export.md](docs/export.md).
- TUI: `:export [sel] [fmt]` writes the session to
  `~/.local/share/rex/exports/<slug>-<shortid>.<ext>` (0600) in the
  background and shows the path when done; the board stays responsive.
- `session_export` in Settings → Features (on). Off, `rex export` exits 5
  and `:export` answers the usual off message.
- TUI: a mobile layout for terminals of 60 columns or fewer: two-line rows
  (name and age, then the text), the short header counts, a three-key help
  line and no summary preview line. `mobile_layout: auto|always|off`
  (Settings → Appearance) controls it.
- TUI: a banner above the agent in the attach popup: what it is doing (the
  AI description, else the task or the question it asks), where (folder),
  for how long (working, waiting, ended) and how fresh the description is.
  With the overview on it also shows what the other sessions need
  (`elsewhere: 2 to review · 1 needs you`). Long text scrolls as a ticker.
  `attach_banner` is `marquee` (default), `static` or `off`; `reduce_motion`
  makes it static, `::banner` hides or shows it for this run, and it gives way
  below 25 rows. The agent's terminal is one row shorter while it shows.
  `rex attach` from a shell has no banner.
- TUI: a Waiting section below Needs you for working sessions that are
  blocked on a shell command, a subagent or a web call, not on you (Claude
  Code). A session moves there after 3 seconds on the call. It has its own
  `◷` marker, which turns once a second (still with `reduce_motion`), a
  count in the header (`N need you · N waiting · N working · N done`) and on
  the screensaver, and `shell 42s` in the detailed view. No sound, no
  notification. Switchable in Settings → Features (`waiting_section`); off,
  those sessions stay in Working.
- Sessions carry `waiting_on` (`shell`, `subagent`, `web`) and
  `waiting_since` while a working session waits on a tool (snapshots,
  patches, `meta.json`). The state stays `working`, so older clients show
  those sessions in Working. The daemon records them even with
  `waiting_section` off.
- Claude Code hooks: `PostToolUseFailure` joins the state hooks, and tool
  waits are bounded: a failed tool call, any other main-thread tool and a new
  prompt all end a Waiting state. Parallel subagents keep it until the last
  one stops.
- `rex ls` shows `waiting (shell)` in the STATE column (text and md) for a
  session in Waiting, and counts it in the header. `-o txt` and the agent
  `state=` key keep `working`; `rex status` is unchanged.

- Skills per session: `NewSession` takes `name` and `skills`. Skills come
  from a library at `~/.config/rex/skills/<name>/SKILL.md`; the daemon copies
  the chosen ones into the session's own folder and starts Claude Code with
  `--plugin-dir`, so nothing is written to `~/.claude`. The registry gains
  `skill_args` (with `{plugin_dir}`; built-in `claude` only, Antigravity has
  no per-session plugin flag), and `rex-daemon` a `-skills-dir` flag. A
  resumed session loads its skills again. Switchable in Settings → Features
  (`spawn_skills`): off, skills are refused and no `--plugin-dir` is added.
- CLI: `rex new -name "text" -skill a -skill b` sets the display name and
  loads skills for that session (exit 5 for an unknown skill, a tool that
  cannot load skills, or when `spawn_skills` is off). `rex skills` lists the
  library in every output format. See `docs/howto/skills.md`.
- TUI: a dim preview line under the board shows the selected session's full
  AI summary and how old it is (`↳ Editing api/webhooks.go … · 12s`), in
  both views, while any session has a summary. In the detailed view it only
  appears when the row cuts the text. Two lines on a tall terminal at
  `summary_detail: long`. Hidden below 20 rows, and switchable in Settings →
  Features (`summary_preview`).
- TUI: descriptions are cut on a word boundary with an ellipsis, and the
  description animation ends on exactly the settled text.
- Setting `summary_detail` (AI summary): `short`, `medium` (default) or
  `long` sets how much an AI description says (about 8, 15 or 28 words; 60,
  110 or 180 characters). The daemon applies a change within about 5
  seconds, with no restart, by polling `config.yaml`.
- Sessions carry `description_at`, when the AI description was last written
  (snapshots, `description` patches, `meta.json`). Older clients ignore it.
- Settings: a new Features section (after Behavior) holds one on/off switch
  per optional feature. A feature that is off is inert: it starts nothing,
  writes nothing, its keys do nothing and its commands refuse. No switches
  yet; each feature adds its own. `settings.FeatureOn(store, id)` reads one.
- `NewSession.trust_folder` (daemon protocol): trusts the session's folder
  for that session only, as if it were in `auto_trust_dirs`. The daemon
  answers the agent's trust dialog (or passes `trust_args`), and
  `skip_permissions` applies there. Refused (`bad_intent`) for relative
  paths, `..`, a filesystem root and the home folder. The choice is saved in
  `meta.json` (`trust_folder` in summaries) and kept when the session is
  resumed. Set it with `rex new -trust`, ctrl+t in the wizard or `:trust`.
- Setting `board_filter` (Features, on by default): switches off the tool
  filter chips, `:filter`, `t`, the `z` chords and `board_hidden_sections`.
  Off, there is no chip row and `:filter` answers `off: enable in Settings
  → Features (rex config set board_filter true)`.
- Setting `search` (Features, on by default): switches off board search.
  Off, `/` does nothing, `:search` answers the off message, and a saved
  query is ignored.
- Setting `undo_complete` (Features, on by default): switches off `uc` and
  `:uc`. Off, the chord is unbound and the command answers the off message;
  `:undone` still unmarks.
- Setting `resume` (Features, on by default): switches off resume ids. Off,
  the daemon assigns and captures no id, the column and `:rid` go away, and
  `Resume` is refused with `resume is off`.
- TUI: the detailed view shows each session's resume id (first 8 characters)
  in a column that is the first to go on a narrow terminal, and `:rid [sel]`
  shows the full id on the status line.
- The daemon tracks a resume id per session (`resume_id` in snapshots,
  state patches and `meta.json`). A Claude Code session is started with
  `--session-id <uuid>`, and its SessionStart hook keeps the id current
  after `/clear` or an in-app `/resume`. Tools declare how their id is
  found and used in a new `resume` registry block (`args`, `new_args`,
  `capture_regex`, with `{id}` as the placeholder); see `docs/registry.md`.
  Only canonical UUIDs are accepted, since the id ends up on a command line.
 
- TUI: the detailed view (`v`) has a start→end span column (`14:02→15:30`,
  `14:02→` while live). The end is when the row entered Done: the earlier of
  marking it done and the session ending. It is the first column to go on a
  narrow terminal.
- Setting `board_hidden_sections` (Behavior): section ids hidden at startup,
  for example `done`. `:bg` now also saves the hidden sections, and the
  next start restores them.
- TUI: the filter bar names the hidden sections (`done hidden (zh)`) and
  stays up while any is hidden. `:prune` says "filters ignored" in its
  confirmation while a filter is active, since it clears every Done
  session.
- TUI: `:filter hide <section…>`, `:filter show <section…|all>` and
  `:filter only <section…>` hide and show board sections by id, prefix,
  title or number. A bare `:filter` clears the tool filter, every hidden
  section and the search.
- TUI: board search. `/` opens an editor on the bottom line and narrows the
  board as you type: a row must contain every word, case-insensitive, in
  its name, task, AI summary, tool, model, slug, short id, fleet, folder
  name or resume id. `enter` keeps the query, `esc` restores the previous
  one, and `esc` on the board clears it. The filter bar shows
  `/auth · 3 matches · esc clear` and counts matches inside hidden
  sections. A search unfolds a long Done column. `:search [text]` does the
  same from the command line (`::search` from attach), and `:bg` saves the
  search for the next start.
- TUI: `uc` and `:uc [sel]` (also `:resume [sel]`) undo done: they unmark a
  running session that was marked done (`cc`, `:done`), and resume an exited
  one (done, failed, crashed, or completed with `C`) in place with its
  resume id: same row, slug, start time and transcript, back in Working. An
  exited session whose tool can't be resumed is explained rather than
  touched. A session that is not in Done gets a notice. `u` then `c` never
  arms `cc`.
- TUI: section titles read `n/total` (`WORKING  1/4`) while a tool filter or
  search narrows the rows.
- TUI: hide board sections. `zh` hides or shows the completed (Done)
  section, `z1`-`z9` toggle section N, `z0` shows them all. While `z` is
  pending the help line lists the second keys. `1`, `3` and `tab` aimed at
  a hidden section say how to show it, and rex refuses to hide every
  section.
- `-o/--output text|txt|md|json|agent`, `--agent` and `--no-color` for
  `rex ls` and `rex status` (more commands follow). `json` is a versioned
  `{"v":1,...}` document, `md` a GitHub table, `agent` one `key=value`
  line per record plus an `ok` line, and `txt` the plain columns rex has
  always printed in a pipe. Errors follow the format on stderr. Colors
  follow the `color_scheme` setting, `NO_COLOR`, `CLICOLOR_FORCE` and
  `REX_ASCII` are honored, and stderr is never colored by stdout's
  terminal. New environment: `REX_OUTPUT`, `REX_AGENT`, `REX_ASCII`. The
  existing `-json` stays as a legacy alias with its current shapes.
  Nothing in the UI uses it yet.
- New `Resume` intent (`client.Resume`). On a running session it removes the
  done mark. On a done, failed or crashed session, including ones stranded by
  a daemon restart, it starts the agent again in the same row with the tool's
  resume arguments, keeping the id, slug, start time and transcript; folder
  trust is recomputed from the current config. It fails with `bad_state` when
  the tool has no resume support or no resume id is known, and with
  `too_many_sessions` at the session cap. A `Delete` racing a resume leaves no
  process and no directory behind. See `docs/protocol.md`.
- The daemon records `ended_at` for every session: the first time it reached
  done, failed or crashed. It is in `meta.json`, in snapshots and in state
  patches, and older sessions get it from their last event.
- `rex setup` asks whether agents may skip the folder trust prompt in a
  folder (default no; it suggests the folder that holds the current
  project), and `rex setup -auto-trust folders` sets it without the
  question. Both write `auto_trust_dirs` and refuse the values the daemon
  ignores (a filesystem root, the home folder, relative paths, `..`), so
  setup and the daemon share one set of checks.
- TUI: screensaver. After 5 minutes with no key or click on the bare
  board, the board gives way to a small centered `∴ rex` with the live
  counts under it (`1 need you · 3 working · 7 done`). Any key or click
  brings the board back and does nothing else. Overlays, prompts and the
  attach popup never time out. On macOS it keeps the display awake while
  it shows (`caffeinate -d -w <rex pid>`, stopped on wake and on exit).
  It moves a little once a minute, and stays put with `reduce_motion`. New
  settings: `screensaver_enabled` (default on),
  `screensaver_after_minutes` (default 5, 1–120) and
  `screensaver_keep_awake` (default on, macOS only).
- Antigravity CLI (`agy`) replaces Gemini CLI, which Google retired on
  2026-06-18, as a built-in tool (`antigravity`). It ships the account's
  current models (Gemini 3.8 Flash with low/medium/high effort, Claude
  Sonnet/Opus 4.6, GPT-OSS 120B) and takes the first prompt with
  `--prompt-interactive`. Under `auto_trust_dirs` it starts with the folder
  trusted for that session only (`--settings`, never saved), so no trust
  question shows. Tools can declare this with the new `trust_args`. Its
  state comes from the screen's footer, using the new optional
  `busy_regex` so a quiet stretch mid-turn stays working. `gemini` stays
  built in but hidden, so existing setups keep working.
- TUI: a Claude Code session running subagents shows how many in its row
  (`⋔3`). The daemon tracks them with Claude's `SubagentStart` and
  `SubagentStop` hooks; the wire gains an optional `subagents` field.
- TUI: marking a session done (`cc`, `:done`, `rex done`) plays the done
  sound. The session keeps running, so nothing announced the mark before.
  `cc` no longer also plays the close sound for a mark; undoing still does.
- `rex prune` and the TUI's `:prune` delete every Completed session (done,
  failed, crashed, marked done) after one confirmation, keeping sessions
  marked done whose agent is still working. Each session is checked again
  just before its delete, so one typed into after the list was shown is
  kept. `rex prune -dry-run` lists them; `-yes` skips the question and is
  required without a terminal.
- `auto_trust_dirs` setting: for sessions started in these folders, the
  daemon answers Claude Code's "trust this folder" dialog (it moves to
  "Yes, I trust this folder", checks it is selected, then confirms).
  Empty by default; set it with `rex config set auto_trust_dirs ~/dev`.
  Tools declare their dialog with `trust_prompt` in `tools.yaml`. Only
  list folders whose code you trust; see `SECURITY.md`.
- Protocol: the daemon answers every intent that carries an envelope `id`
  with exactly one reply: its specific event (`Hello` gets `Snapshot`),
  an `Error`, or the new `Ack` event. Intents without an `id` behave as
  before. `Snapshot` carries the daemon's version (`daemon_version`) and
  `Hello` the client's. See `docs/protocol.md`.
- Hardening effort under way: a staged pass over crash, hang, data-loss and
  security findings from a full code audit. Entries for each fix land here.
- `make lint-windows`, `make cover`, `make cover-check`, `make fuzz` and
  `make test-e2e`. `make check` now also runs the Windows lint and the
  per-package coverage floors in `tools/coverage/baseline.txt`.
- CI: nightly race stress run of the daemon and wire tests, and a weekly
  fuzz run.
- `SECURITY.md` (private reporting and threat model), `CONTRIBUTING.md`,
  CODEOWNERS and a pull request template.
- `rex doctor` shows the installed agy version (`agy 1.5.5`, a `-verbose`
  row) and warns below 1.5.5 (untested). It shows nothing when agy is not
  installed, and gives up after 2 s.
- Overview (`overview_enabled`, off by default): a header line, `:overview`
  and `rex overview` show what needs you, what is ready for review and what
  finished, with a one-line summary from the local model. The counts and
  lists come from rules over session fields; only the headline is model
  text. It runs at most about twice a minute, only while a `rex` board is
  open, and off it starts nothing. Claude sessions that finished a turn now
  count as ready for review (`wait_reason`, from the Stop and Notification
  hooks).
- Wire (additive, `v` stays 1): an `Overview` event, `Snapshot.overview` and
  `wait_reason` on sessions; `Hello.client` `rex-tui` now marks a board
  viewer. Older clients ignore them.

### Fixed

- Daemon: a transient error accepting a client connection (for example
  "too many open files") no longer shuts the daemon down and stops every
  running session. It retries with backoff (5 ms up to 1 s) and logs it.
- Daemon: a full disk no longer kills running agents. When an agent's
  `transcript.log` can't be written, the session keeps running (attach and
  the board still see all its output); the transcript pauses, retries every
  30 s, and the daemon log records how many bytes are missing from it.
  Before, the agent was killed and marked failed.
- Settings: saving a `config.yaml` that is a symlink (a dotfiles manager
  such as stow) writes the linked file and keeps the link, instead of
  replacing the link with a plain file. A dangling link is an error. Two
  saves at once (the settings page and `rex config set`) no longer share
  one temp file, so neither fails with "no such file".
- TUI: a delete confirmation, rename or attach popup for a session that
  another client deleted (or that is gone after a reconnect) now closes
  with a note, instead of acting on an id the daemon no longer knows. A
  session that arrives in both a snapshot and its own add event is listed
  once.
- TUI: `tui-state.json` is written to a temp file and renamed into place,
  so a crash or full disk while saving can't leave a truncated file. The
  failures overlay reads only the last 256 KiB of the daemon log instead
  of the whole file.
- `rex update` reads the new version with plain output, so `CLAUDECODE`,
  `REX_AGENT` or `REX_OUTPUT` in the caller no longer turn it into an agent
  or json line.
- `rex setup -preview` shows the shell block on separate lines in every
  format.
- `rex resume -print` prints the bare id under `CLAUDECODE` auto-detect.
- ANSI color no longer reaches a redirected stderr: each stream decides on
  its own, and nothing uses a shared renderer.
- `rex digest` and `rex stats` read their colors from the theme instead of
  copied hex values that ignored `color_scheme`.
- Daemon: `Resize` and `NewSession` sizes are clamped to 20-1000 columns and
  5-500 rows. A client asking for 65535x65535 made the daemon allocate about
  4 GB for its screen mirror and could run it out of memory.
- TUI: the attach popup is no longer wider than a narrow terminal (it was
  clipped below 54 columns), and in the mobile layout (60 columns or fewer)
  it is a full-screen sheet with no margin or padding.
- TUI: the help, settings, new-session, stats, overview and failures
  overlays no longer overflow a narrow terminal (below about 60 columns);
  wide lines are cut with `…` instead of being clipped by the terminal.
- TUI: `b` on the wizard's describe step now goes back when the field is
  empty, as the footer says. It used to be typed into the text.
- `auto_trust_dirs` and the per-session trust request now also refuse a case
  variant of the home folder on a case-insensitive filesystem (macOS,
  Windows), and any folder that holds the home folder (`/Users`, `/home`).
  Folders are compared by identity on disk, and ancestors by whole path
  elements, so `/Users2` is still fine.
- TUI: `::undone` in the attach popup only unmarks; on a session that is not
  marked done it shows a notice instead of marking it done.
- Short ids match in any case (`rex attach A91C`). `rex --help` no longer
  lists the `@needs`, `@working` and `@done` selectors, which never existed.

- With no daemon running, commands now say so: `rex-daemon is not running
  (no socket at <path>); start it with \`rex daemon start\``, instead of the
  raw `dial unix …: no such file or directory`. The same goes for a socket
  file a dead daemon left behind. A daemon that accepts the connection but
  fails the handshake exits 4 (it was 1) in selector lookup, `rex prune` and
  `rex render`, which also returned raw errors.

- `rex wait` refuses an unknown `-until` and a negative `-timeout` (exit 5)
  instead of waiting forever or without limit. A timeout is recognized by
  its error type, not its text. `docs/cli.md` said a session removed during
  `wait` exits 3; it exits 2.

- `rex new`, `rm` and `prune` no longer crash when stdin is closed, and a
  character device that is not a terminal (`< /dev/null`, `< NUL` on
  Windows) is no longer mistaken for one.

- `rex new -cwd` makes a relative folder absolute and refuses one that
  does not exist or is not a folder (exit 5). The daemon now rejects a
  `NewSession` whose `cwd` is not absolute (`bad_intent`) instead of
  resolving it against its own working directory.

- `rex setup -tool/-model/-effort` now writes `default_spawn_tool`,
  `default_spawn_model` and `default_spawn_effort` (before, they were
  ignored). An unknown tool, model or effort exits 5 and lists the valid
  ids. The setup wizard writes the same keys, refuses a model the tool does
  not have, and keeps every other setting in an existing config.yaml.

- Descriptions no longer freeze after a burst of output, an Ollama outage or
  a state change: requests are queued per session and deferred instead of
  dropped, the agent going idle with a question is summarized, and a slow
  call no longer blocks every session for 15 s (each attempt has its own
  timeout, and only transient errors are retried).
- A finished session gets one final summary instead of keeping stale
  "Running tests…" text.
- Antigravity: the `Generating...` spinner and the text typed in its input
  box no longer show as the board's last line or reach the summary prompt.
- The settings page showed a second "Spawn" section for
  `auto_trust_dirs`; it now sits with the other Spawn settings.
- TUI: a reconnect to the daemon no longer resets the tool filter to `all`.
- TUI: `:filter <tool>` now moves the selection off a row the filter hides
  (before, `enter` did nothing) and rejects a tool that is neither a filter
  chip nor on the board, with `filter: unknown tool or subcommand x`.
- `rex ls` no longer prints raw slugs and names: control and bidi
  characters are flattened, like in the TUI.
- The attach popup draws Antigravity (and other ultraviolet-based agents
  under a tmux, ghostty or kitty `TERM`) correctly: the logo is centered and
  no row is cut off on the right. rex's terminal emulator ignored the "repeat
  last character" sequence (`CSI n b`) and lost its tab stops beyond the
  width a screen was first created with, so every later cell on a row was
  drawn too far left or jammed at the right edge. It now handles both, and
  `CSI ? 5 W`.
- A session that ends now always sends `working_since: null` in its final
  state patch, as the protocol says. It was lost on the path where the child
  exits or is completed, so a client could keep a stale working timer.
- A partial `tools.yaml` entry for a retired built-in tool (codex, gemini,
  ollama, grok, deepseek, kimi) no longer stops the daemon from starting.
  It is dropped with a warning in the daemon log and on the TUI splash.
- Saving settings no longer overwrites changes made elsewhere. `Store.Save`
  re-reads `config.yaml` and writes only the keys that store changed (a
  reset deletes its key), so a TUI save can no longer undo a concurrent
  `rex config set`. New config files hold only the keys that were set; keys
  this build does not know are kept. Existing files are not trimmed.
- `rex config set auto_trust_dirs` (and the settings page) now refuse the
  values `rex setup` refuses (a filesystem root, the home folder, relative
  paths, `..`), exit 5 and leave `config.yaml` unchanged, instead of saving
  a value the daemon ignores.
- `rex ls` and `rex digest` clip long model, slug and name columns by
  display width instead of bytes, so multi-byte text such as `·` or CJK
  is no longer cut mid-character and the columns stay aligned.
- TUI: the `λ` prompt glyph, the `▸` selection marker and the `:` prompt
  are now colored and follow `color_scheme` (they were never colored).
  The text cursor is drawn in reverse video, or as a solid `█` when there
  is no color (`NO_COLOR`), so it is always visible. The color scheme and
  the style cache are no longer written from background commands at
  startup, which removes a data race with the splash screen.
- TUI: `:undone` on a session that is not marked done no longer marks it
  done. It now only unmarks, and says "<name> is not marked done"
  otherwise. `:done` still toggles.
- TUI: opening a running full-screen agent (Claude Code, codex, gemini, …)
  no longer shows a black popup until the agent next draws. The popup
  attaches without the transcript replay and relied on the agent redrawing
  on `^L`, which a busy agent, or one that ignores `^L`, did not do. The
  daemon now repaints its own copy of the agent's screen into the popup
  (colors, cursor and terminal modes included) when it opens. `rex attach`
  gets the same repaint after the transcript tail, which could start
  mid-frame (or was empty with `-no-replay`). The `^L` nudge is now sent
  only to a daemon too old to repaint. Protocol: `Subscribe` gains an
  optional `repaint` field and `SessionOutput` an optional `repaint` flag;
  older clients and daemons are unaffected. Subscribing no longer loses or
  repeats the output that arrives while the daemon reads the replay.
  `rex attach` also resets the scroll region, charset and cursor-key and
  keypad modes on detach.
- A character that arrives split across two reads of the agent's output
  (box drawing, emoji, accented letters) is no longer lost, along with the
  rest of its row shifting left, in the attach popup and in the daemon's
  copy of the screen (board last line, AI summaries, the repaint above).
- TUI: the attach popup no longer drifts out of sync with a long-running
  Claude Code session until you detach and reattach. The terminal emulator
  read Claude's kitty keyboard sequences (`CSI < u`, `CSI > 5 u`) as
  "restore cursor", so each redraw landed on the wrong rows and the popup
  kept showing an old frame. Sequences with a `<`, `=` or `>` marker are now
  ignored. The daemon's per-session screen, which feeds the board's last
  line and the AI summaries, had the same drift and is fixed too.
- TUI: the attach popup renders about 5× faster with 5–10× fewer
  allocations per frame (211×57: 3.2 ms to 0.6 ms on an unchanged screen,
  3.6 ms to 0.8 ms with new output), so busy agents no longer make it choppy. It
  paints the emulator grid directly instead of through a lipgloss style
  per cell, reuses unchanged rows, and draws the popup border in one pass.
- TUI: the attach popup shows bold, italic, underline and reverse text,
  including an agent's own reverse-video cursor, and 24-bit colors. It
  used to drop the attributes and send 24-bit colors as invalid 256-color
  codes.
- Daemon and TUI: the terminal emulator no longer allocates on almost
  every byte of agent output (14× fewer allocations on escape-heavy
  output). The daemon runs one per session.
- CLI: when the daemon is unreachable, the error names the socket once
  (`dial unix <path>: connect: …`) instead of wrapping it twice.
- `rex reload` and the TUI's `:reload` report a `tools.yaml` the daemon
  could not load and exit 1. They used to exit 0 with the error only in
  the daemon's log. A daemon older than this release still gets the
  reload, unacknowledged.
- Paths: every rex path now comes from one package
  (`internal/runtime/paths`, documented in `docs/paths.md`). File
  locations are unchanged. An unset or relative `HOME` is now an error
  that names the flag to pass (`-socket`, `-config`, `-state-dir`),
  instead of reading and writing paths relative to the working directory
  (or, for `rex digest`, a shared `/tmp/rex`). A relative
  `XDG_RUNTIME_DIR` is ignored, as the XDG spec requires.
- `.gitignore` no longer hides source files: bare `store_test.go`,
  `persist_test.go` and `test/` patterns silently ignored matching files
  anywhere in the tree.
- The CLI exit code is now found through wrapped errors.
- Daemon: a client that stops reading no longer freezes the daemon. Each
  connection has its own bounded outbound queue.
- Daemon: shutdown completes while clients are connected, and a second
  SIGINT or SIGTERM forces the exit.
- Daemon: a second `rex-daemon` on the same state directory exits and
  names the running daemon's PID instead of taking over its socket.
- Daemon: a panicking intent handler closes only its own connection.
- Daemon: a full disk while writing a transcript no longer orphans the
  agent process or hangs Delete. The agent is killed and reaped.
- Daemon: a daemon started by `rex` or the TUI survives Ctrl-C in that
  terminal, leaves no zombie process, and an early crash is reported with
  the end of its log.
- TUI: when the daemon goes away, the TUI reconnects with backoff instead
  of spinning at 100% CPU.
- TUI: `:new` no longer locks up the TUI.
- TUI: `rex setup` no longer crashes on a broken `tools.yaml`; it falls
  back to the built-in tools.
- TUI: queued sessions show in the Working column and in the header
  counts.
- TUI: a data race when the summarizer model is swapped is fixed.
- TUI: `:reload` reloads the daemon on the TUI's own socket instead of
  the default one, and a failed reload shows in the status line.
- CLI: `rex uninstall -strip-profile` no longer truncates a profile with
  an unterminated `# BEGIN REX`. It writes a `.rex-backup-<unix>` copy,
  keeps symlinks, CRLF line endings and long lines, and also scans
  `~/.profile`.
- CLI: `rex uninstall` stops the daemon and confirms it is gone before it
  removes anything. The new `-force` flag continues anyway.
- CLI: `rex update` refuses a binary-swap install before it stops the
  daemon.
- CLI: `rex attach -read-only` can detach with Ctrl+].
- CLI: `rex log` strips every escape sequence, including in `-f` mode.
- CLI: commands no longer print log lines into the terminal or the attach
  screen.
- CLI: `rex daemon stop` no longer refuses when the user runs more than one
  daemon (for example on different sockets).
- Tests: the e2e suite no longer fails at random under `go test -race
  ./...` on macOS. Every script hard-linked the same `rex-daemon`, and
  Gatekeeper killed the daemons when it looked the shared file up by the
  path of a script that had already finished. Each script now runs its
  own copy.
- Tests: the PTY orphan tests no longer send SIGKILL to a PID read back
  from a file unless that process is still the test's own child.

### Security

- rex now requires Go 1.26.11: it fixes GO-2026-6859 in `net/http`,
  which govulncheck reports as reachable from rex.
- The daemon's state directory, socket and logs are owner-only (0700
  directories, 0600 files), and existing ones are tightened. Connections
  from other users are rejected. The TUI state file, the daemon log the
  TUI and setup open, and the directories `rex doctor`, `rex config edit`
  and the boot splash create are owner-only too.
- Saving settings or installing the shell block no longer makes
  `config.yaml` or a shell profile world-readable: a rewrite keeps the
  file's existing mode, and a new file is 0600.
- Session IDs are validated before any path is built from them, which
  blocks path traversal.
- Protocol lines are capped at 16 MiB. Protocol errors are reported to the
  client before the connection is closed, with the new codes
  `line_too_long`, `unsupported_version` and `internal_error`.
- Session titles, fleets, summaries and session text are stripped of
  terminal escape sequences and control characters, both when the daemon
  receives them and when the TUI renders them. The daemon, the TUI and the
  AI summarizer share one sanitizer, so the TUI now also removes bidi
  overrides and drops C1 escape sequences (such as U+009B) whole.
- Supervisor logs redact agent command-line arguments.
- A runaway `init.lua` can no longer hang the daemon: Lua handlers run off
  the hot path with timeouts. `init.lua` is documented as trusted code.
- `rex daemon stop`, `rex reload` and the TUI's `:reload` act only through
  the daemon's socket. They never signal a process found by name.
- `auto_trust_dirs` entries that start with `~/` and contain `..` are now
  ignored like any other path with `..`. Expanding `~/` cleaned the path
  before the check, so `~/..` trusted the folder above home (such as
  `/Users`) and `~/a/../b` trusted `~/b`. A `HOME` that contains `..` no
  longer lets `~/` trust the home folder, and while `HOME` is set but
  cannot be resolved, no folder is trusted.

## [v1]

Baseline release: `rex` CLI and TUI, `rex-daemon` PTY supervisor, JSONL
protocol over a Unix socket, tool registry, Lua hooks, and native Windows
support (ConPTY).

[Unreleased]: https://github.com/TristanBietsch/rex/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/TristanBietsch/rex/releases/tag/v2.0.0
[v1]: https://github.com/tristanbietsch/rex/commits/6a6e0a1
