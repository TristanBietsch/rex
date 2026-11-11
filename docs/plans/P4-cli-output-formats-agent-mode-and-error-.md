# P4 CLI output, formats, agent mode and error log

## Summary
- Add one small rendering kit in `cli/core`. Each command defines its data once (a typed payload plus a `core.View` made of Header, KV, Table, Checks and Note blocks). One `Printer`, set up once per process, renders that data as `text | txt | md | json | agent`. The styles come from a shared theme palette (P4 takes over U1 step 4).
- Agent mode (`--agent`, `-o agent`, `REX_AGENT=1`) is built for LLM agents: one line per record in `key=value` form, then an `ok …` trailer. No color, no prompts. Errors are one line on stderr: `error code=N kind=… msg=…`. Agents find it through `rex --help` and through `rex agent-help`, a compact usage card.
- Add a private error log: JSONL in `~/.local/state/rex/cli-errors.log`, 0600, with redacted arguments, capped at 1 MiB plus one backup, and an opt-out setting. `rex errors` reads it in every format. P4 builds on P3: `core.ParseArgs`, `ErrHelpShown`, width-aware `Truncate`, the H9 messages and the H12 exit 5.

## Current behavior
All of this was checked with `go run ./cmd/rex` (and a built binary) under `HOME=$(mktemp -d)`. The P3 lane-C worktree was also read: `core/args.go` (`ParseArgs`, `ParseTextArgs`, `ErrHelpShown = ExitError{Code: 0}`) and `output.go` (`Truncate`/`padCells` built on `x/ansi`) are already in place.

**Error path**
- `cmd/rex/main.go:17-23`: every error is printed as `rex: <err>` to stderr, and `exitCodeFor` (`:97-103`) maps it to an exit code.
- `ExitError` is just `{Code, Msg}` (`core/version.go:20-31`). It has no hint, no kind and no wrapped cause, so callers flatten the cause into a string.
- Captured:
  ```
  $ rex status
  rex: dial unix /var/folders/…/T/tmp.9vc6LGWVwg/.cache/rex/rex.sock: connect: no such file or directory
  exit=4
  $ rex frobnicate
  rex: unknown command "frobnicate" (try `rex --help`)
  exit=1          # P3-H12 changes this to 5
  ```
- Errors are never JSON, even when `-json` is passed.

**Styling is copied hex values, ignoring the theme**
- `core/help.go:10-21`, `inspect/digest.go:23-46` (plus off-palette `#4CAF82 #F0A04B #E05C5C`), `inspect/stats.go:~217-224`, `setup/doctor.go:66-71` (`#4ADE80 #FACC15 #F87171`), `setup/setup.go:97-104`, `setup/update.go:22-28` (`#5FC96E`), `setup/uninstall.go:20-23` (`#4CAF50`).
- None of these read `color_scheme`.
- Everything goes through lipgloss's *global* renderer, which looks only at stdout. Styled text sent to stderr therefore follows stdout's TTY state.
- There is no NO_COLOR, CLICOLOR_FORCE or TERM=dumb code in rex. Today lipgloss/termenv handle these implicitly, for stdout only.
- No CLI code enables Windows VT processing.

**`rex --help`** (`help.go:33-140`). Pipe capture, with ANSI stripped by lipgloss:
```

  ✦ rex — Runtime Executive for Agents
  ────────────────────────────────────────────

  USAGE
    rex                          launch the TUI
    rex <command> [flags]

  SESSION
    ▸ ls                     list sessions
    ▸ new                    create a new session (wizard)
  …
  · selectors: short id, slug, @needs, @working, @done
```
- `new` is described as a "wizard", which is wrong.
- The `@` aliases do not exist (P3-H10 removes them).
- There is no output or format section.

**Hidden terminal query on every TTY run.** bubbletea v1.3.10 `tea_init.go:21` calls `lipgloss.HasDarkBackground()` in a package `init`. Because `cmd/rex` links the TUI, *every* rex command run with stdout on a terminal writes an OSC 11 query and a cursor-position query. Captured from `rex version` under `script`:
```
^[]11;?^[\^[[6nv1
```
- It is gated by termenv (`termenv_unix.go:233-275`): skipped for TERM=screen*/tmux*/dumb, for a non-TTY stdout and for a background process. Agents with piped stdout are never hit.
- Humans pay one round-trip, or up to `OSCTimeout = 5s` on terminals that answer neither query.
- P4 cannot fix this from inside rex. See the follow-ups.

**`rex ls`** (`core/output.go` `WriteSessionsTable`; `session/ls.go:12-55`). Fixed `%-Ns` columns. From `e2e/testdata/script/session_new.txtar`:
```
ID     STATE         TOOL       MODEL                 SLUG                      LAST EVENT  NAME
1a2b   done          echo       short                 x-tool-echo-model-short…  3s ago
```
- TOOL is 9 wide, so `antigravity` (11) pushes the rest of the row out of line.
- Slug and Name are printed without `protocol.FlattenDisplayText`. The TUI and `prune.go` do flatten them; the CLI relies only on daemon-side sanitizing.
- `-short` and `-show-archived` are no-ops.

**`rex status`** (`inspect/status.go`): `0 awaiting input · 0 working · 2 completed`. It exits 1 with an *empty* message when a session needs input; `main` suppresses the empty message.

**`rex doctor`** (`setup/doctor.go:240-300`). Captured:
```

  ∴ rex doctor
  ────────────────────────────────────────────

  ! rex binary         PATH has /Users/tristan/.local/bin/rex but running /var/folders/…/rex
  ✓ rex-daemon binary  /Users/tristan/.local/bin/rex-daemon
  ! daemon reachable   socket not responding — run: rex daemon start  (socket=/var/folders/…/rex.sock)
  ✓ config dir         /var/folders/…/.config/rex
  …
  · all required checks passed, 2 warnings
```
- Long details are never wrapped, and paths are absolute.
- `-json` is a compact object and includes INFO rows that the text view hides.

**`-json` shapes are inconsistent and unversioned**

| Command | `-json` today |
|---|---|
| `ls` | JSONL, one SessionSummary per line |
| `config list` | JSONL |
| `status` | `{"awaiting_input":…}` object |
| `doctor` | one-line `{"checks":[…],"ok":…}` |
| `digest`, `stats` | `MarshalIndent` (multi-line), with string durations (`"active_duration":"1h 24m"`) |

- There is no `v` field.
- `-json` is missing from new, wait, fleet, daemon status, version and others.

**Prompts and progress go to mixed streams**
- `rm` prompts on stderr (`rm.go:36`).
- `prune` prompts on its `errOut`.
- `update` prompts `upgrade now? [Y/n]` on **stdout** (`update.go:153`), and `uninstall` also prompts on stdout (`uninstall.go:327`).
- `update` prints progress `· git pull ...` to stdout (`:327-332`).

**Message style varies.** `done.go` uses `—`, `daemon.go:193-206` uses `·`, and `new.go` prints `7d4f\tslug`, which scripts and e2e depend on.

**Stale flag spelling in messages.** `digest.go` errors say `--date`/`--since`, and `setup.go:~165` prints `--tool`. Go's flag package accepts both spellings, but the docs use the single dash.

**No error record.** `core/logging.go` discards slog for the CLI, so a failed command leaves no trace once the terminal scrolls.

## agy look analysis
These observations come only from `agy --version` (1.5.5), `agy --help` and `agy <sub> --help`, run with and without a PTY. A string and glyph scan of the binary was used for the glyph list. No session was started.

**Help is plain and calm, even on a TTY.** It emits no ANSI and no banner.

**Layout**
- The first line is `Usage: agy mcp <subcommand> [flags] [args]`, then a blank line, then a one-sentence description, then a blank line.
- Section headers are Title case with a colon (`Flags:`, `Commands:`, `Available subcommands:`) at column 0.
- Entries are indented 2 spaces. The name column is padded to the longest name plus 2, and descriptions line up.
- A long synopsis wraps, and its description continues on the next line at the description column (`plugin validate [path] [--strict] [--json]`).
- Help ends with a single hint: `Run "agy mcp <subcommand> --help" for flags, argument rules, and examples.`
- Spacing: one blank line between sections and none at the top.

**Formats.** List commands offer `--json` (`plugin list [--json]`), and print mode has `--output-format text|json|stream-json`.

**Weaknesses rex should not copy**
- The top level uses Go's raw `Usage of agy:` while subcommands use `Usage:`.
- `agy mcp add --help` falls back to the top-level help, so it has no per-subcommand help.

**Stack and glyphs.** agy is built on lipgloss and bubbletea, the same stack as rex. Its glyph set includes ✓ ✗ ⚠ ● ○ ▌ ⎿ ▸ ❯ and braille spinner frames. Its interactive TUI colors could not be observed under the read-only rules (see the open questions).

**What rex adopts**
- agy's structure: Usage-first help, Title-case section headers with a colon, 2-space indent, aligned columns, hanging wraps, one closing hint, no banners or horizontal rules, and whitespace for separation.
- A small, consistent glyph vocabulary.
- Color only for meaning (state, severity, accent on command names), taken from rex's U1 palette (`default` with violet accent `#B392F0`, per D5).
- Per-subcommand help everywhere, which P3 already makes exit 0.

## Design

### A. Rendering kit (`internal/surface/cli/core`)

| File | Contents |
|---|---|
| `format.go` | `type Format uint8` (`Text, TXT, MD, JSON, Agent`), `ParseFormat`, `FormatSet`, `ResolveFormat(flags, env, isTTY)` |
| `printer.go` | `Printer{Format; Out, Err io.Writer; OutTTY, ErrTTY, Color, ErrColor bool; Width int; Now func() time.Time; Styles Styles; Glyphs Glyphs; Cmd string}`; `core.Out()` (process singleton); `InitOutput(args, environ)`; `(*Printer).Emit(Result) error`; `Progress(msg)` (stderr, text only); `Warn`; `Confirm(question) (bool, error)` |
| `view.go` | block types (below) plus `Cell{Text string; Raw any; Tone Tone}` |
| `render_text.go`, `render_txt.go`, `render_md.go`, `render_json.go`, `render_agent.go` | one renderer per format, each walking `View` |
| `agentline.go` | `EncodeLine(kind string, kv ...KV) string`: the agent grammar |
| `styles.go` | `Styles()` built from `theme.Palette` (U1-4 absorbed): `Bold, Title, Dim, Muted, Accent, HR, OK, Warn, Err, State[protocol.State]` |
| `glyphs.go` | Unicode and ASCII sets |
| `term.go` | extends P3's `term.go` with `StdoutWidth()`, `IsTerminal(fd)`, `enableVT()` (Windows) |
| `errors.go` | `ExitError` gains `Hint string` and `Err error` (`Unwrap`); `Kind(code)`; `Errorf(code, hint, format, …)`. `NewExitError` is unchanged |
| `errlog.go` | the error log (section D) |

**Blocks** (a `View` is an ordered `[]Block`):
- `Header{Title, Meta string}`: one line, title in Bold and meta in Dim, followed by a blank line.
- `Section{Title}`: Title-case header with a colon at column 0.
- `KV{Kind string; Pairs []Pair{Key, Label string; Val Cell}}`: labels in Dim, aligned to the longest label plus 2. Long values wrap with a hanging indent at the value column (text only).
- `Table{Kind string; Cols []Col{Key, Title string; Align; Flex bool; Min, Max int}; Rows [][]Cell; Empty string}`:
  - Text: a Dim uppercase header, 2-space gutters, no borders and right-aligned numbers.
  - `Flex` columns shrink to fit `Width`, with an ellipsis, using P3's `Truncate`/`padCells`.
  - All other columns get their natural width.
- `Checks{Rows []Check{Name, Status, Detail, Hint}}`:
  - Glyphs are ✓ pass (OK), ! warn (Warn), ✗ fail (Err) and · info (Muted).
  - A hint goes on its own line, `› cmd`, under the detail.
- `Note{Tone, Text}`: one dim line; tones are info, warn and success.
- `Hint{Text, Cmd}`: a closing line `› Cmd  Text`, with Cmd in Accent. At most one per view.
- `Line{Tone, Glyph, Text}`: a one-line confirmation for actions, for example `✓ started 7d4f fix-auth`.

**Spacing rules (text)**
- No blank line at the top. Exactly one blank line between blocks. The output ends with a single `\n` and no trailing blank line.
- Headers and sections sit at column 0; bodies are indented 2 spaces.
- Width is `term.GetSize(stdout)`, else `$COLUMNS`, else 80, clamped to [40, 200].

**Glyphs**
- Session states reuse the TUI legend: ◆ needs, the first frame of the configured spinner for working (`⠋` for braille), ◌ queued, ● done, ✕ failed, ○ crashed. The CLI and the board stay identical. After U2 these come from the theme's `glyphs`.
- The ASCII set (`! * . o x -`, with `v` for pass) is used in `txt` always, and in `text` when `REX_ASCII=1` or on a Windows console without `WT_SESSION`/`TERM_PROGRAM`.
- ⚠ is avoided because its cell width is ambiguous.

**Color decision** (made once, per stream):
- Color is on only when **all** of these hold: the format is `text`; the stream is a TTY; `NO_COLOR` is unset or empty; `TERM != dumb`; and `--no-color` was not given.
- `CLICOLOR_FORCE=1` (non-empty, not `0`) forces color even in a pipe, for `less -R`.
- Each stream gets its own `lipgloss.NewRenderer(w)`. A disabled stream gets `SetColorProfile(termenv.Ascii)`. Nothing uses the global renderer, so ANSI never reaches a redirected stderr.
- On Windows, `termenv.EnableVirtualTerminalProcessing` runs on stdout and stderr when they are TTYs. The restore function is deferred in `main`.

**Palette.** `Styles()` reads the theme lazily, and only when color is on, so plain output never loads settings.
- The name comes from `settings.LoadDefault()`: `color_scheme`, or `theme` after U2-3 (U2's alias rule: `theme` wins).
- An unknown name falls back to `default`.
- Mapping (from U1 §2): Bold/Title = Fg, Dim = FgDim, Muted = FgMuted, Accent = Accent, HR = Border, OK = Success, Warn = Warn, Err = Danger, and State[...] = the state tokens.

**Untrusted text.** Every `Cell.Text` passes through `protocol.FlattenDisplayText` inside the kit, in every human format. This is the same backstop the TUI uses. Commands cannot forget it.

### B. Output formats

**Flags and environment**
- One global flag, `-o/--output text|txt|md|json|agent`.
- Aliases: `-json` (legacy shape, see below), `--agent` (= `-o agent`) and `--no-color`.
- Environment: `REX_OUTPUT=<format>`, `REX_AGENT=1|0`, `NO_COLOR`, `CLICOLOR_FORCE`, `REX_ASCII`.

**Wiring**
- `core.AddOutputFlags(fs, supported FormatSet) *OutputFlags` registers the flags on each command's FlagSet. They work anywhere among the arguments, thanks to P3's `ParseArgs`.
- `main` also accepts them *before* the verb (`rex -o json ls`): `core.InitOutput(args, os.Environ())` strips leading global flags, pre-resolves the format and stores the process `Printer`.
- Flag `Set` calls update the same singleton. After parsing, `out.Validate()` checks the format against the command's `FormatSet`.

**Precedence**
1. Explicit flags (the last one wins; `-json` together with `--agent` → exit 5 "conflicting output flags").
2. `REX_AGENT=1`.
3. `REX_OUTPUT`.
4. Agent auto-detect (section C).
5. The default: `text` when stdout is a TTY, else `txt`.

**Unsupported format** → exit 5: `rex: attach: output "json" is not supported (supported: text)`.

**Format definitions**
- **text**: the human layout from section A. Styled when color is on. Wraps to `Width`. Progress and hints are allowed.
- **txt**:
  - Plain and stable for `grep`/`awk`: never ANSI, ASCII glyphs, no Header or Hint blocks, no wrapping.
  - Tables use the column widths in use today, truncated by display width (P3-H11), so `rex ls | awk` keeps working. KV is `label  value`, aligned. A Section is printed as `# Title`, which is exactly what `config list` prints today.
  - Byte-compatible with today's pipe output for `ls`, `status`, `config list/get`, `new`, `prune -dry-run`, `daemon status` and `fleet ls`. The e2e regexps stay green.
- **md**:
  - GitHub-flavored. Header → `### Title`, plus meta in italics.
  - Table → a pipe table with a `|---|` row. Cells escape `\ | `` ` `` < >` and a leading `#`, and newlines are already flattened.
  - KV → a two-column `| Key | Value |` table. Checks → a table with a `Status` column (PASS/WARN/FAIL/INFO). Note → `> text`.
  - Meant for pasting into issues and PRs.
- **json**:
  - One compact document plus `\n` on stdout: `{"v":1,"cmd":"ls","data":<payload>}`. Some commands add `"meta":{…}` (for example `{"source":"disk"}` for digest when the daemon is down).
  - `v` changes only on a breaking change; adding fields does not change it.
  - Field names are snake_case. Times are RFC 3339 UTC. Durations are integers named `*_ms`. Session objects reuse `protocol.SessionSummary`'s wire tags, which are already a stable contract.
  - Errors produce **nothing on stdout**. stderr gets `{"v":1,"error":{"code":4,"kind":"daemon_unreachable","cmd":"ls","msg":"…","hint":"…"}}`.
  - Payloads come from `Result.Data`, never from display strings.
- **agent**: section C.

**Legacy `-json`.** It keeps today's exact shapes (JSONL for `ls` and `config list`, indented output for digest/stats, and so on) through `Result.Legacy func(io.Writer) error`, so existing scripts and `config.txtar` keep working. The docs mark it as legacy (see the open questions). Commands that never had `-json` do not get it; they get `-o json` only.

**Command classes and support**

| Class | Commands | Formats |
|---|---|---|
| Query | `ls`, `status`, `digest`, `stats`, `fleet ls/show`, `config list/get`, `doctor`, `daemon status`, `version`, `errors`, `help`/`agent-help`, `update -check`, `setup -preview`, `uninstall -dry-run`; later `overview` (S4), `skills` (P1), `theme list/show` (U2), `trust -ls` (P2) | all 5 |
| Action | `new`, `rm`, `rename`, `name`, `done`, `undone`, `archive`, `complete`, `reply`, `send`, `wait`, `prune`, `fleet set/unset`, `config set/reset`, `reload`, `daemon start/stop/restart`; later `resume` (T3), `trust` (P2), `theme new` (U2) | all 5 (result record) |
| Content | `log` (transcript bytes), `completion`, `render`, `config edit` | text and txt; `agent` behaves like txt (raw content, no trailer); json/md → exit 5 |
| Interactive | `rex` (TUI), `attach`, `setup` (wizard), `update` and `uninstall` without `-yes` | text only. Any other format → exit 5 with a hint (`use rex ls`, `rex log <sel> -n 50`, `rex send`, `setup -yes`) |

**Action results.** A typed result (for example `{"session":{…}}`) and a `Line` view:
- text: `✓ started 7d4f fix-auth · claude opus · high`
- txt: today's line (`7d4f\tfix-auth`, `m1: renamed to m2`)
- agent: `ok cmd=new id=7d4f slug=fix-auth …`

**Prompts and progress**
- `Printer.Confirm` prompts on **stderr**, and only when the format is text **and** stdin and stderr are both TTYs.
- Otherwise it returns `ExitOperationRefused` (6) with a hint naming `-force`/`-yes`. This generalizes P3-H7 to `prune`, `update` and `uninstall`, and moves their prompts off stdout.
- `Progress` goes to stderr in text only.

**Stream rule.** stdout carries the result only. Errors, warnings, prompts, progress and process notes go to stderr. Notes that qualify the data (for example "on-disk snapshot") are part of the result: a Note in text, `meta.source` in json, `source=disk` in agent.

**Broken pipe.** Writing to a closed stdout ends the process with SIGPIPE (Go's default). That is never logged as an error.

### C. Agent mode
**Line grammar.** `<kind> key=value key=value …`
- Keys are lowercase snake_case, in a **fixed order per kind** (documented).
- A value is bare when it matches `[A-Za-z0-9._:/@+%-]+`. Otherwise it is Go-quoted with `strconv.Quote`, which escapes ESC, newlines and non-printables, so a value can never break the line. Empty is `""`.
- Durations are integer seconds (`idle_s=42`). Times are RFC 3339 UTC. Booleans are `true` or `false`.

**Output shape**
- Lists: one line per record (`session …`, `check …`, `fleet …`, `setting …`, `error_entry …`), then a trailer `ok cmd=ls count=3`.
- Queries and actions: exactly one line, `ok cmd=status needs_input=1 working=2 queued=0 done=3 failed=0`. The existing exit code is unchanged (status still exits 1 when a session needs input).
- Errors: one line on **stderr**, for example `error code=4 kind=daemon_unreachable cmd=ls msg="rex-daemon is not running" hint="rex daemon start"`, with the normal exit code.
- Kinds: 1 error, 2 not_found, 3 ambiguous, 4 daemon_unreachable, 5 invalid_args, 6 refused, 7 timeout.

**Implied behavior**
- No color, no spinner and no progress output.
- Never prompts. Destructive commands need `-force` or `-yes`, else exit 6.
- Interactive commands exit 5 with a hint.
- `rex --help` in agent mode prints the agent card.

**Auto-enable (recommended option b in the open questions).**
- Agent mode turns on when `REX_AGENT=1`, *or* when `CLAUDECODE=1` and stdout is not a TTY. Claude Code's Bash tool sets `CLAUDECODE`, including inside rex-spawned Claude sessions.
- A human at a terminal is never affected, because the TTY check fails.
- `REX_AGENT=0` turns it off.
- Not adopted:
  - Guessing other agents' environment variables, which are unverified.
  - Injecting `REX_AGENT` into the environment of spawned children. It adds no capability and would advertise cross-session control (see the S4 security note).

**Discoverability**
- `rex --help` gets an `Output:` section with a line `agents: rex agent-help (or REX_AGENT=1)`.
- `rex agent-help` (alias `rex help agent`) prints a card of at most 30 lines and about 1.2k tokens:
```
rex agent card v1: supervise coding-agent sessions (claude, agy) from a shell.
mode: --agent or REX_AGENT=1. One line per record, no color, never prompts.
line: <kind> key=value…  bare [A-Za-z0-9._:/@+%-]+ or "quoted"; empty ""
end:  stdout "ok cmd=<c> …" | stderr "error code=N kind=K msg=… hint=…"
exit: 0 ok 1 error 2 not_found 3 ambiguous 4 no_daemon 5 bad_args 6 refused 7 timeout
sel:  short id (4+ hex) | slug | full id
read  rex ls [-state S] [-tool T]   -> session id state tool model effort slug idle_s name
      rex status                    -> ok needs_input working queued done failed (exit 1 if needs_input>0)
      rex log <sel> -n 50           -> transcript text, ANSI stripped
      rex wait <sel> -until done|needs_input|any -timeout 10m
write rex new -tool claude -model opus [-cwd D] [-name N] -- <prompt> -> ok cmd=new id slug
      rex reply <sel> -- <text> | rex send <sel> -- <text>
      rex done <sel> | rex rm <sel> -force | rex prune -yes
never rex (TUI), rex attach, rex setup without -yes: interactive, exit 5 here
more  rex <cmd> -h · rex errors -n 5 · formats: -o json|txt|md
```
  `rex agent-help -o json` emits the same information as data: commands, arguments, flags, formats, kinds and fields.
- Docs: new sections in `docs/cli.md`, a row in `docs/agents/interfaces/cli.md`, and a `docs/howto/agents.md` snippet that users can paste into their own CLAUDE.md or AGENTS.md. rex never writes to user files.

### D. Error log
**Where.** `rexlog.Path("cli-errors")`, which is `~/.local/state/rex/cli-errors.log` (or `$REX_LOG_DIR`). It sits next to `daemon.log` and the other `<name>.log` files. Add a `CLIErrorLog` row to the `paths` package doc and to `docs/paths.md`.

**When.** In `main`, after `run` returns `err != nil`, unless:
- the error is `ErrHelpShown`;
- it is a *signal* `ExitError` with an empty message (status needs-input, doctor failures);
- it is `REX_ERROR_LOG=0`;
- or the setting `cli_error_log` is false. The setting is read only on this path, and a load failure means "on".

**Record.** One JSONL line per error, written in a single `Write` call and capped at 4 KiB:
```json
{"v":1,"time":"2026-11-11T09:12:03.123Z","version":"v1","os":"darwin/arm64","pid":4242,
 "cmd":"rm","args":["<sel:4 bytes>","-force"],"format":"agent","agent_auto":true,
 "tty":{"in":false,"out":false},"exit":6,"kind":"refused",
 "error":"rm: stdin is not a terminal; pass -force","chain":["…","…"],"hint":"rex rm <sel> -force"}
```
- `chain` is built by walking `errors.Unwrap` (and `Unwrap() []error`), up to 8 entries.
- Each message goes through `FlattenDisplayText`, is capped at 512 runes, and passes a secret scrubber that replaces values matching `sk-…`, `ghp_/gho_/github_pat_…`, `AKIA[0-9A-Z]{16}`, `xox[abps]-…`, `Bearer …` and `(password|token|secret|api[_-]?key)=…` with `<redacted>`.

**Argument redaction.**
- The logic of `daemon/pty/argv.go:redactArgv` moves to a new pure package, `internal/runtime/redact`: `Argv(argv []string, p Policy) []string`, with `Policy{KeepValues map[string]bool; FreeTextFrom int; Selectors int}`.
- `pty.redactArgv` becomes a one-line wrapper with zero policy, so its existing tests stay unchanged.
- The CLI policy keeps:
  - the verb and flag names;
  - the values of allowlisted flags (`tool model effort o output until timeout state after n since date`);
  - selectors as `<sel:N bytes>`, plus the literal when it is a short hex id.
- The CLI policy redacts:
  - everything from the free-text position onward (`new`, `reply`, `send`, `name`, `config set`'s value, P1 `-name`);
  - all other values, including `-cwd` and `-socket`.
- Never logged: stdin, prompts, transcripts and environment values.

**Rotation.** Before appending, if the file is 1 MiB or larger, rename it to `cli-errors.log.1` (overwriting) and start fresh.
- Concurrent rex processes rely on `O_APPEND` single writes. A rotation race can lose a few lines, which is documented and acceptable.

**Permissions.** `rexlog.PrivateDir` and `OpenPrivate` (0700 dir, 0600 file). A user-chosen `REX_LOG_DIR` is not chmodded, matching rexlog.

**Failure.** Logging never changes the exit code and never prints, except one stderr line when `REX_LOG_LEVEL=debug`.

**Opt-out.** A new setting `cli_error_log` (bool, default true) in the Advanced section, placed after `lua_config_path` so the section stays contiguous (orchestration override 6). The env variable `REX_ERROR_LOG=0` also turns it off.

**Viewer.** `rex errors [-n 20] [-since 24h] [-clear] [-o …]`
- It reads `.log` and `.log.1`, newest first.
- text: a table `TIME  EXIT  CMD  ERROR` plus a hint.
- json: an array of records. agent: `error_entry …` lines.
- `-clear` truncates both files. It asks first in text mode and needs `-force` otherwise.

**doctor row.** `cli errors`: info `none in 24h` or warn `N in 24h · rex errors`. Appended after P2's `checkAutoTrustDirs`, extending override 16.

### E. Hardening on top of P3

| ID | Fix |
|---|---|
| K1 | One stdout/stderr rule (section B). Prompts in `update.go:153` and `uninstall.go:327` and progress in `update.go:284-332` move to stderr. |
| K2 | No ANSI in pipes, for either stream: per-stream renderers, so the lipgloss global renderer is never used. |
| K3 | Width: `StdoutWidth()` with a `COLUMNS` fallback; text tables flex-shrink; KV, check details and help descriptions wrap with hanging indent; nothing wraps in txt/md/json/agent. |
| K4 | All untrusted cells are flattened in the kit (`ls` Slug/Name are raw today). |
| K5 | Windows: VT enabled when on a TTY; ASCII glyphs on a legacy console; `make lint-windows`; tests cover the CRLF-free output. |
| K6 | Interactive commands refuse non-text formats (exit 5); `Confirm` refuses without TTYs (exit 6). |
| K7 | Messages use single-dash flags (`digest.go` `--date/--since`, `setup.go` `--tool`); `help.go` text is fixed (`new` is not a wizard). |
| K8 | Deterministic rendering for goldens: injectable `Printer.Now`; sorted map iteration. |

### F. Contract for every other CLI builder (T3-7, P1-4, P2-3, S4-8, U2-10, U3/A1/P2 doctor rows)
1. Parse with `core.ParseArgs` (P3), and register `core.AddSocketFlag` and `core.AddOutputFlags(fs, core.AllFormats)`, or the set for your class.
2. Never call `fmt.Print*` or `os.Stdout` directly, and never use `lipgloss.Color("#…")`. `TestNoHardcodedHex` and the `TestNoDirectStdout` AST test fail the build.
3. Return `core.Out().Emit(core.Result{Cmd, Data, View})`.
   - `Data` is a typed struct with snake_case JSON tags (sessions as `protocol.SessionSummary`).
   - `View` uses only kit blocks.
   - The agent line comes from `Table.Kind`/`KV.Kind` plus the column keys; there is nothing extra to write.
4. Errors: `core.Errorf(code, hint, …)`. Never print errors yourself; `main` renders them per format and logs them.
5. Prompts go through `core.Out().Confirm`; destructive actions take `-force` (alias `-yes`/`-y`).
6. Add a golden archive `testdata/golden/<cmd>.txtar` covering every format; add the verb to `help.go`'s table, the agent card and `completion.go`.
7. Doctor rows keep returning `checkResult`. P4 converts them to a `Checks` block in one place, so A1, U3 and P2 append unchanged.

`rex ls -wide` (T2/T3) adds `Col`s with `Flex:false` to the same `Table`; txt appends them before NAME, keeping NAME last.

## Mockups (plain text; color noted in brackets)

**`rex ls`**
Before (TTY and pipe, both the same):
```
ID     STATE         TOOL       MODEL                 SLUG                      LAST EVENT  NAME
a91c   needs_input   antigravity  gemini-3-pro          refactor-db               12s ago
7d4f   working       claude     opus · high           fix-auth-flow             2m ago      Auth fix
```
After, text at 80 columns:
```
Sessions  1 needs you · 1 working · 1 done                   [Bold / Dim]

     ID    STATE      TOOL         MODEL         SLUG           IDLE  NAME   [Dim header]
  ◆  a91c  needs you  antigravity  gemini-3-pro  refactor-db     12s         [◆ Needs amber]
  ⠋  7d4f  working    claude       opus · high   fix-auth-flow    2m  Auth fix
  ●  0b2e  done       claude       sonnet · med  write-tests      1h  ✓ marked done [● Success, badge Dim]

› rex attach <id>  open a session                            [› Accent]
```
- At 40 columns MODEL and SLUG shrink with `…`, and NAME drops into a second dim line.
- txt (pipe) is unchanged from before, with width-correct alignment from P3-H11.
- agent:
```
session id=a91c state=needs_input tool=antigravity model=gemini-3-pro effort="" slug=refactor-db idle_s=12 name=""
session id=7d4f state=working tool=claude model=opus effort=high slug=fix-auth-flow idle_s=120 name="Auth fix"
ok cmd=ls count=3
```
- md: `### Sessions` followed by `| ID | State | Tool | Model | Slug | Idle | Name |` rows.
- json: `{"v":1,"cmd":"ls","data":[{"id":"…","short_id":"a91c",…}]}`

**`rex status`**
- Before: `0 awaiting input · 1 working · 2 completed`
- After, text: `◆ 1 needs you   ⠋ 1 working   ● 2 done` [state colors]
- txt: the same as before, byte for byte (shell prompts parse it).
- agent: `ok cmd=status needs_input=1 working=1 queued=0 done=2 failed=0` (exit 1).

**`rex doctor`**
Before: see Current behavior. After, text at 80 columns:
```
rex doctor  v1 · darwin/arm64

  ✓ rex binary         ~/.local/bin/rex
  ! daemon reachable   not responding (~/.cache/rex/rex.sock)
                       › rex daemon start
  ✓ config dir         ~/.config/rex
  ✓ settings load      ~/.config/rex/config.yaml
  ! rex binary         PATH has ~/.local/bin/rex but this is
                       /private/var/folders/xr/…/rex      [hanging wrap]

2 warnings · all required checks passed                      [Warn]
```
agent:
```
check name="daemon reachable" status=warn detail="not responding" hint="rex daemon start"
…
ok cmd=doctor fails=0 warns=2
```
(The final line is `fail cmd=doctor …` when checks fail.)

**`rex --help`**
After, text:
```
rex — Runtime Executive for Agents  v1

Usage:
  rex                       open the board (TUI)
  rex <command> [flags]

Sessions:
  ls                        list sessions
  new [flags] -- <prompt>   start a session (-tool -model -cwd)
  attach <sel>              open a session (ctrl+] detaches)
  …
Output:
  -o, --output FORMAT       text | txt | md | json | agent
  --agent                   one line per record, no color, no prompts

Selectors: short id (4+ hex), slug or full id.
Run "rex <command> -h" for flags · agents: rex agent-help
```
Command names are in Accent, descriptions in Dim and headers in Bold.

Per-command help (`rex new -h`) is rendered by the kit from the FlagSet, in agy style:
- `Usage: rex new [flags] -- <prompt>`, then the description, then `Flags:` aligned, then `Output:`, then an example.
- P3's ErrHelpShown keeps it at exit 0.

**`rex new` confirmation**
- Before: `7d4f	fix-auth-flow`
- After, text:
```
✓ started 7d4f fix-auth-flow · claude opus · high · ~/code/app   [✓ Success]
› rex attach 7d4f
```
- txt: `7d4f	fix-auth-flow` (unchanged).
- agent: `ok cmd=new id=7d4f slug=fix-auth-flow tool=claude model=opus effort=high`

**An error** (daemon down)
- Before: `rex: dial unix /var/…/rex.sock: connect: no such file or directory`
- After, text (stderr, TTY):
```
✗ rex-daemon is not running                                  [✗ Danger, Bold]
  no socket at ~/.cache/rex/rex.sock                         [Dim]
› rex daemon start                                           [Accent]
```
- txt: `rex: rex-daemon is not running (no socket at /…/rex.sock); start it with rex daemon start` (P3-H9 text).
- agent: `error code=4 kind=daemon_unreachable cmd=ls msg="rex-daemon is not running" hint="rex daemon start"`
- json: `{"v":1,"error":{"code":4,"kind":"daemon_unreachable","cmd":"ls","msg":"…","hint":"rex daemon start"}}`

## Files to change
**New**
- `internal/surface/cli/core/{format,printer,view,render_text,render_txt,render_md,render_json,render_agent,agentline,styles,glyphs,errors,errlog}.go` plus tests.
- `internal/catalog/theme/{theme.go,builtin.go}`: the Palette struct exactly as U1 §1, with values for `default` (= today's classic at first), `classic`, `noir`, `paper`.
- `internal/runtime/redact/redact.go`.
- `internal/testutil/golden/golden.go`: a txtar golden helper with `-update` and ESC shown as `␛`, shared with U1-1.
- `internal/surface/cli/inspect/errors.go` (`rex errors`), `internal/surface/cli/meta/agenthelp.go`.
- `testdata/golden/*.txtar` per package.
- `docs/howto/agents.md`.

**Modified**
- `cmd/rex/main.go`:
  - `InitOutput` before `run`;
  - per-format error rendering and `errlog.Record` in `main()`;
  - the cases `errors` and `agent-help`, and `help agent`;
  - the TUI refusal in agent mode.
  - P3 owns `main()`; P4 edits it only after P3-10.
- `core/{help.go (rewrite on kit), output.go (WriteSessions* become view builders; the legacy writers are kept for -json), term.go (P3), version.go, socket.go (hints)}`.
- Migrated to `Emit`:
  - every `session/*.go`;
  - `inspect/{status,digest,stats,fleet,log,render}.go`;
  - `setup/{doctor,config,setup,update,uninstall}.go`;
  - `lifecycle/{daemon,reload}.go`;
  - `meta/{complete,completion}.go` (verbs).
- `internal/daemon/pty/argv.go` (a wrapper around `redact.Argv`); `internal/catalog/settings/registry.go` (`cli_error_log`); `internal/runtime/paths/paths.go` (doc row only).
- `tools/coverage/baseline.txt`: add `internal/catalog/theme` and `internal/runtime/redact`; raise `cli/core`.
- e2e: new `output_formats.txtar`, `agent_mode.txtar`, `error_log.txtar`; `help_version.txtar` updated.

## Test plan (test first; each step's tests fail before its code)

**Unit (`core`)**
- `TestResolveFormat`, table-driven over flags, env and TTY:
  - precedence;
  - a conflict → 5;
  - an unsupported format → 5;
  - `CLAUDECODE=1` with a TTY → text, and without a TTY → agent;
  - `REX_AGENT=0` beats `CLAUDECODE`.
- `TestColorDecision`: NO_COLOR, empty NO_COLOR, TERM=dumb, `--no-color`, CLICOLOR_FORCE, and stderr decided independently of stdout.
- `TestNoANSIWhenNotTTY`: every format × every golden view; the output never contains `\x1b`. txt/md/json/agent also ignore CLICOLOR_FORCE.
- `TestEncodeLine` and `FuzzAgentLine`: every output is one line; parsing it back gives the input; ESC, newline and bidi are always quoted.
- `TestMarkdownEscape` (`|`, backtick, `<script>`).
- `TestJSONEnvelopeV1`, and error JSON on stderr with empty stdout.
- `TestFlattenAllCells`: a slug with `\x1b]0;pwn\a` renders inert in every format.
- `TestWidthClamp`; `TestTableFlexShrink` at 40, 80 and 120 columns (no line wider than the width; NAME last in txt).
- `TestConfirmRefusesNonTTY` (exit 6) and `TestConfirmOnStderr`.
- `TestLegacyJSONByteIdentical`: `ls`, `status`, `config list`, `doctor`, `digest`, `stats` legacy writers versus today's output.

**Goldens.** One `.txtar` per view, with sections `text.w40`, `text.w80`, `text.w120`, `text.color.w80` (TrueColor, ESC→␛), `txt`, `md`, `json`, `agent`.
- Views: help, `<cmd> -h` (×3 samples), agent-help, version, ls (empty, mixed states, a 60-character name, CJK), status, new, rm, rename, name, done, undone, archive, complete, reply/send, wait, prune (dry-run and summary), fleet ls/show/set, config list/get/set, doctor (pass, warn and fail), digest (live and disk), stats, daemon status, reload, update -check, setup -preview, uninstall plan, errors, plus one error view per exit code.
- Built from pure `xxxView(data, now)` builders with a fixed `Now`. A few `Run*` integration tests run against a `testutil` daemon.

**Theme and styles**
- `TestCLIStylesFollowTheme` (paper → paper Fg; replaces U1's planned test).
- `TestNoHardcodedHex` and `TestNoDirectStdout` (go/ast over `surface/cli`, excluding core printer files and tests).

**Error log**
- `TestErrLog_RecordsAndRedacts`: `new -- "my secret prompt"` → no prompt bytes on disk; `sk-abc…` scrubbed.
- `_Mode0600` (testperm), `_RotatesAt1MiB`, `_OptOutSettingAndEnv`, `_SkipsHelpAndSignals`, `_WriteFailureSilent`, `_ConcurrentAppend` (16 goroutines, every line valid JSON).
- `redact`: today's `pty` argv tests, moved, plus the CLI policy table.

**e2e**
- `output_formats.txtar`: `ls`/`status` in each `-o`, `-json` legacy lines, `-o yaml` → 5, `attach -o json` → 5.
- `agent_mode.txtar`: `REX_AGENT=1 rex ls` ends with `ok cmd=ls`; `rm` without `-force` → 6 with an `error code=6` line on stderr; a bare `rex` → 5.
- `error_log.txtar`: a failing command → `rex errors -o agent` shows it.
- Windows: `make lint-windows`, plus the `glyphs` ASCII fallback test with a faked legacy-console environment.

## Docs/CHANGELOG
- `docs/cli.md`: new sections OUTPUT FORMATS (matrix, precedence, env), AGENT MODE (grammar, kinds, auto-detect, card), ERROR LOG (path, record, opt-out, `rex errors`); ENVIRONMENT lists `REX_OUTPUT REX_AGENT REX_ASCII REX_ERROR_LOG NO_COLOR CLICOLOR_FORCE COLUMNS`; each command's synopsis gets `[-o FORMAT]`; `rex errors` and `rex agent-help` sections.
- `docs/agents/interfaces/cli.md` and `modules/cli.md` (the kit contract from section F), `docs/agents/AGENTS.md` ("drive rex with `REX_AGENT=1`; `rex agent-help`").
- `docs/STRUCTURE.md` (`catalog/theme`, `runtime/redact`), `docs/paths.md`, `docs/settings.md` (`cli_error_log`), `docs/howto/agents.md`.
- `SECURITY.md`: the error log as in-scope local data, with what it never contains.
- CHANGELOG `[Unreleased]`:
  - **Added:** `-o/--output`, agent mode, `rex agent-help`, `rex errors`, the error log, the `cli_error_log` setting, CLI colors that follow the theme.
  - **Changed:** pipes get `txt` (no banners or glyphs in piped help/doctor/digest/stats); `update`/`uninstall` prompts move to stderr and need `-yes` without a TTY; off-palette CLI greens and reds unified.
  - **Fixed:** raw slugs and names in `rex ls`; ANSI on a redirected stderr.

## Conflict map and wave placement

| Task | Interaction | Rule |
|---|---|---|
| P3 (lane C, in progress) | Shares `core`, `main.go` and every command file | P4 builds on `ParseArgs`, `ParseTextArgs`, `ErrHelpShown`, `Truncate`/`padCells` and `term.go`. P4 never re-fixes H1-H16. Command migrations (P4-7..9) go **after P3-4..11**, which edit the same files. `main.go` edits go after P3-10 (H12). P3-H9 hint text becomes `ExitError.Hint`. |
| U1 | U1-4 (CLI styles) | **Absorbed by P4** (P4-2/P4-3). P4-2 *creates* `catalog/theme` with U1's Palette struct and today's values. U1-3 then only extends it (new tokens, contrast tests, TUI migration) and must not rename fields. U1-5's new `default` reaches the CLI automatically. U1-1 reuses `testutil/golden`. U1's `TestNoHardcodedHex` covers the TUI; P4's covers the CLI. |
| U2 | `theme` rename, `rex theme` (U2-10), glyphs | `core.Styles()` reads `theme` after U2-3 (one-line change in U2-3). `rex theme list/show/new` follows the section F contract. Theme `glyphs` feed `core.Glyphs`. |
| T2/T3 | `rex ls -wide`, `rex resume` (T3-7) | Extra `Col`s in the ls Table; `resume` is an Action-class command. If T3-7 lands before P4-5, migrate it in P4-7. |
| S4 | `rex overview` (S4-8) | Query class: KV header plus a Table per bucket; agent `bucket=` lines. |
| P1 | `rex new -name/-skill`, `rex skills` (P1-4) | `-name` value redacted in the error log; `skills` is a Query table. |
| P2 | `rex trust`, `new -trust`, `checkAutoTrustDirs` | Action and Query classes; the doctor row appends before P4's `cli errors` row. |
| U3 / A1 | doctor rows | Unchanged `checkResult` API; order is A1 → U3 → P2 → P4 `checkCLIErrors`. |
| T3-1+A1-1 (`childEnvFor`) | none | P4 does not inject `REX_AGENT`. |

**Recommended placement.** Split P4 so the contract exists before the new verbs are written.
- **Wave 2, lane C, first (before S1-1): P4-1 … P4-5.** Core-only and additive: the golden helper, the theme skeleton, Printer and formats, the kit and error rendering, and `ls`/`status` as the reference migration.
  - This needs P3-1 and P3-2 (done on lane C).
  - It must land before T3-7 (wave 3), S4-8 and P1-4 (wave 4), P2-3 (wave 5) and U2-10 (wave 7).
- **Wave 4, lane C, after P3-11: P4-6 … P4-13.** Migrations, error log, `rex errors`, auto-detect, docs. This pushes A1-4..7 slightly into wave 5, where it already sits.
- Hard prerequisites to add to `00-review.md`:
  - P3-2 → P4-3.
  - P4-4 → T3-7, S4-8, P1-4, P2-3, U2-10.
  - P3-11 → P4-7.
  - P4-2 → U1-3 (U1-3 becomes "extend").
  - U2-3 → swap `color_scheme`→`theme` in `core.Styles` (inside U2-3).

## Security notes
- **Error log.**
  - It holds only command names, allowlisted flag values, a short-id/selector size marker, the exit code and kind, and scrubbed, flattened, capped error text.
  - It never holds prompts, reply/send text, `config set` values, cwd or socket paths, stdin, environment or transcripts.
  - 0600 in a 0700 dir (rexlog helpers).
  - Rotation bounds disk use to 2 MiB.
  - The scrubber is defense in depth on top of the allowlist.
- **Escape injection.** Session strings come from agents and summaries. The kit flattens every cell in human formats. Agent mode Go-quotes values. md escapes HTML and pipes. json escapes control characters. This closes the raw-slug path in today's `rex ls`.
- **Agent mode adds no capability.** It makes the existing CLI easier to drive. Socket access already means "can type into any session" (SECURITY.md). Auto-detect needs a non-TTY stdout, so it cannot silently change a human's terminal. No environment is injected into spawned agents.
- **Destructive commands.** No prompts in non-text formats, so destructive commands need explicit `-force`/`-yes` (exit 6 otherwise). This extends P3-H7.
- **No new network or socket surface.** `CLICOLOR_FORCE` affects only presentation.

## Open questions for user
1. **Which "antigravity look" should rex copy?**
   - (a) agy's command-output structure: plain aligned sections, `Usage:` first, calm, no banners, colored only for meaning with rex's violet palette. **Recommended.**
   - (b) agy's interactive TUI chrome; you would share a screenshot, since it couldn't be captured read-only.
   - (c) keep rex's current banner style (`∴ rex doctor` plus a rule) and only tidy it.
2. **Agent-mode shape?**
   - (a) one line per record plus a final `ok …` line. **Recommended.**
   - (b) the whole result on one line (records separated by ` ; `).
   - (c) JSON Lines.
3. **Turn agent mode on automatically?**
   - (a) only with `--agent`, `-o agent` or `REX_AGENT=1`.
   - (b) also when `CLAUDECODE=1` and stdout is not a terminal. **Recommended.**
   - (c) whenever stdout is not a terminal.
4. **Existing `-json` flags?**
   - (a) keep today's shapes as a legacy alias, with new `-o json` versioned `{"v":1,…}`. **Recommended.**
   - (b) make `-json` mean `-o json` now (breaks scripts; CHANGELOG note).
   - (c) keep the legacy shape for one release with a stderr deprecation note, then switch.
5. **Default output in pipes?**
   - (a) `txt`: today's stable columns, no banners or glyphs. **Recommended.**
   - (b) `text` without color (banners and glyphs kept).
   - (c) `agent`.
6. **Error-log viewer command?**
   - (a) `rex errors`. **Recommended.**
   - (b) `rex log -errors`.
   - (c) both.
7. **Error-log default?**
   - (a) on, 1 MiB plus one backup, opt out with `cli_error_log: false`. **Recommended.**
   - (b) off until enabled.
   - (c) on, but only unexpected failures (exit 1 and 4).

## Ordered steps (each one commit; `make check` green)
**Wave 2, lane C**
1. `test(testutil): txtar golden helper with -update` (`internal/testutil/golden`; used by P4 and U1-1).
2. `feat(theme): palette package with today's schemes` (`catalog/theme`, Palette per U1 §1; `default`=classic values; `TestEveryBuiltinComplete`; coverage floor). No TUI change.
3. `feat(cli): output format resolution and per-stream printer`:
   - `format.go`, `printer.go`, `styles.go` (U1-4 absorbed), `glyphs.go`, `term.go` width and VT;
   - global `-o/--agent/--no-color/-json` registration; `InitOutput` in `main`;
   - tests for precedence and color.
4. `feat(cli): view kit, five renderers, structured errors`:
   - blocks, text/txt/md/json/agent, `EncodeLine` and fuzz, `ExitError.Hint/Err`, `Errorf`;
   - per-format error printing in `main`; kit goldens.
5. `refactor(cli): ls and status on the kit` (reference implementation; legacy `-json` and txt byte-identical; e2e `output_formats.txtar`; flatten fix).

**Wave 4, lane C (after P3-11)**
6. `feat(cli): help, per-command help and agent card on the kit` (`help.go` rewrite, FlagSet-driven `-h`, `rex agent-help`/`help agent`, md/json help, completion verbs).
7. `refactor(cli): session commands on the kit; Confirm on stderr` (new, rm, rename, name, done, undone, archive, complete, reply, send, wait, prune, fleet set/unset).
8. `refactor(cli): inspect commands on the kit` (digest, stats, fleet ls/show, log/render format guards).
9. `refactor(cli): setup and lifecycle commands on the kit; theme-only colors`:
   - doctor `Checks`, config, setup -preview, update/uninstall prompts to stderr and `-yes` in non-TTY, daemon, reload, version;
   - `TestNoHardcodedHex` and `TestNoDirectStdout` for `surface/cli`.
10. `feat(cli): private error log with redacted args` (`runtime/redact` plus the pty wrapper, `errlog.go`, `cli_error_log` setting, rotation).
11. `feat(cli): rex errors and doctor error row`.
12. `feat(cli): agent mode auto-detect and interactive guards` (CLAUDECODE + non-TTY, TUI/attach/setup refusals; `agent_mode.txtar`).
13. `docs(cli): output formats, agent mode, error log; changelog` (all the docs listed above; raise coverage floors).

## Size estimate
**L.** 13 commits, about 2.5k lines of Go plus about 1.5k lines of goldens. Steps 1-5 (wave 2) are roughly 40% of the work, and the step 7-9 migrations are mostly mechanical.

**Follow-ups (out of scope):**
- bubbletea's init-time OSC 11/CPR query on every TTY run (`tea_init.go:21`). The fix is the bubbletea v2 migration, or a `third_party` replace with the init removed, with the query moved into `tui.Run` (U2's `auto` theme already plans a synchronous query there).
- `rex ls -short`/`-show-archived` no-ops (P3-H14 marks them deprecated).

### Critical Files for Implementation
- /Users/tristan/Documents/personal/dev/rex/cmd/rex/main.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/cli/core/output.go (plus the new printer.go, view.go and render_*.go next to it; P3's args.go is on feat/w1-lane-c)
- /Users/tristan/Documents/personal/dev/rex/internal/surface/cli/core/help.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/cli/setup/doctor.go
- /Users/tristan/Documents/personal/dev/rex/internal/daemon/pty/argv.go (source for internal/runtime/redact)
