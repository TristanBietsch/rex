---
title: rex boot splash
date: 2026-05-15
status: approved (design)
---

# rex boot splash

## Summary

Add a systemd-style boot splash that renders inside the TUI alt-screen for the
~1 second between typing `rex` and seeing the board. Each real init step prints
a green `[  OK  ]` row with a short description, a few rows carry Japanese
flavor (`接続`, `受信中`, `起動完了`), and every row that pops up plays a brief
chime from the user's selected soundset. Failures render in red with a cause
and a fix hint; the splash freezes and the user can quit.

The splash replaces nothing functionally — it surfaces work that already runs
silently between `rex` and the first frame. The goal is to make that runway
look intentional and to give visible feedback when boot fails.

## Goals

- A self-contained, swappable splash component (`renderSplash`) so the visual
  can evolve without touching the boot pipeline.
- Real, accurate status per step — no fake spinners over no-op work.
- Per-row audio that respects the active soundset and mute setting.
- A minimum visible duration (1.0–1.5 s) so the splash never flashes.
- Graceful failure rendering for terminal errors (daemon won't start, dial
  fails, handshake fails, etc.).

## Non-goals (v1)

- Animated logos / ascii-art / typing effects beyond the row cascade.
- A retry key for failed boots. (`rex` re-run is the recovery path.)
- Tailing `~/.local/state/rex/daemon.log` inside the splash.
- Configurable splash content. The pipeline and visuals are fixed.
- A skip key. The splash is at most ~1.5 s and is part of the experience.

## Architecture

### Today's boot path

`rex` (no args) → `cli.RunTUI` calls `rexlog.Init`, checks the daemon socket,
spawns `rex-daemon` if needed, then hands off to `tui.Run`. `tui.Run` does the
remaining heavy lifting synchronously *before* calling `tea.NewProgram`:

```go
client.Dial(socket) → c.Hello("rex-tui") → c.Subscribe("") →
settings.NewStore + Load → applyTheme → audio.New + Play(EventStartup) →
LoadTUIState → tea.NewProgram(m, tea.WithAltScreen()).Run()
```

All of that runs while the terminal still shows the user's shell; the alt-screen
only activates after `Run()` is called.

### New boot path

The boot work moves *inside* the Bubble Tea program, dispatched as a sequence
of `tea.Cmd`s. The program starts in `FocusBoot`, renders the splash, and
transitions to `FocusBoard` once two conditions are met (real boot done + min
duration elapsed).

```
rex → cli.RunTUI (slim) → tui.Run
  ├─ sync mini-bootstrap: read sound prefs from settings.json, audio.New(...)
  ├─ build Model{Focus: FocusBoot, Audio: ..., Socket: socket, BootStart: now}
  ├─ tea.NewProgram(m, tea.WithAltScreen()).Run()
  │    Init() → dispatch step 1, start min-duration tick
  │    Update(bootStepMsg) → append row, play chime, dispatch next step (or finish)
  │    Update(bootMinElapsedMsg) → flip BootMinDone
  │    When BootDone && BootMinDone && !BootFailed → hand off to FocusBoard
  └─ post-Run: if final.BootFailed → return BootError (rex main prints to stderr)
```

`cli.RunTUI` shrinks to: `rexlog.Init("tui")` + `tui.Run(DefaultSocket())`.
No daemon work in cli.

`daemonReachable`, `daemonStart`, and `findDaemonBinary` are extracted from
`internal/cli/daemon.go` into a new package `internal/daemonctl` so both `cli`
(for the existing `rex daemon start` subcommand) and `tui` (for the splash
pipeline) can call them. `internal/cli/daemon.go` becomes thin wrappers around
`daemonctl` to preserve current CLI behavior.

### Model extensions

```go
// Focus modes — adds FocusBoot to the existing enum.
const (
    FocusBoard Focus = iota
    FocusWizard
    FocusHelp
    FocusSettings
    FocusAttach
    FocusFail
    FocusConfirmQuit
    FocusConfirmDelete
    FocusBoot // new
)

// Splash state on Model.
type Model struct {
    // ... existing fields ...

    BootLog     []bootLine    // accumulated [ OK ] / [ FAIL ] rows
    BootStep    int           // index into bootSequence
    BootStart   time.Time     // for elapsed display + total
    BootMinDone bool          // min-duration tick fired
    BootDone    bool          // last step msg handled, no terminal fail
    BootFailed  bool          // a step returned stepFail
    BootError   error         // populated on terminal fail
}

// Player interface so tests can swap in a recording fake.
type audioPlayer interface {
    Play(event string)
}
```

### Boot pipeline shape

```go
type bootStatus int
const (
    stepOK bootStatus = iota
    stepFail
    stepWarn
    stepSkip
)

type bootStepMsg struct {
    Name   string         // category, e.g. "daemon"
    Status bootStatus
    Desc   string         // free text shown after "·"
    Dur    time.Duration  // displayed when non-trivial
    Err    error          // populated when stepFail or stepWarn
}

type bootMinElapsedMsg struct{}

// bootSequence is a fixed list of step Cmds run in order. Each one is async;
// the next is only dispatched after the previous step's msg is handled.
var bootSequence = []func(m Model) tea.Cmd{
    stepLogInit,
    stepPathsEnsure,
    stepTTYProbe,
    stepSettingsLoad,
    stepThemeApply,
    stepAudioInit,
    stepAudioLoad,
    stepRegistryLoad,
    stepKeymapBind,
    stepSocketResolve,
    stepDaemon,
    stepClientDial,
    stepHandshake,
    stepSubscribe,
    stepSnapshotParse,
    stepStateRestore,
    stepRendererWarm,
}
```

A 70 ms `tea.Tick` runs between each step msg so the rows cascade visibly even
when work is sub-millisecond. The slow step (`stepDaemon` spawning the daemon
binary) consumes its real time and absorbs the delay.

### View

`Model.View()` gains one case:

```go
case FocusBoot:
    return renderSplash(m, w, h)
```

`renderSplash` is *not* wrapped in `centerOverlay` — the splash takes the
whole alt-screen.

## Visual

### Sketch — happy path

```

  ∴ レックス · rex runtime executive
  起動中 booting...  0.47s

  [  OK  ] log.init        · ~/.local/state/rex/tui.log
  [  OK  ] paths.ensure    · config + state dirs ok
  [  OK  ] tty.probe       · 198×52 · truecolor · ja_JP.UTF-8
  [  OK  ] settings.load   · ~/.config/rex/settings.json
  [  OK  ] theme.apply     · noir
  [  OK  ] audio.init      · soundset=evangelion · vol=0.80
  [  OK  ] audio.load      · 12 cues · 起動準備
  [  OK  ] registry.load   · 8 tools (5 enabled · 3 hidden)
  [  OK  ] keymap.bind     · 42 bindings
  [  OK  ] socket.resolve  · /tmp/rex-tristan/rex.sock
  [  OK  ] daemon          · already running (pid 4123)
  [  OK  ] client.dial     · connected · 8ms
  [  OK  ] handshake       · 接続 · rex-tui
  [  OK  ] subscribe       · 受信中 · event stream open
  [  OK  ] snapshot.parse  · 4 sessions · 1 needs · 2 work · 1 done
  [  OK  ] state.restore   · selected=7d4f · filter=all
  [  OK  ] renderer.warm   · styles cached

                                       準備完了 ready · 1.34s
```

### Layout

- Full alt-screen, no border, ~2-char left margin.
- Row 1: blank.
- Row 2: header — `∴ レックス · rex runtime executive`.
- Row 3: spinner — `起動中 booting...  <elapsed>` while running; replaced by
  `準備完了 ready · <total>` once the last step lands. The replacement happens
  before the splash hands off; users see the ready line for the remaining
  fraction of the min-duration window.
- Row 4: blank.
- Rows 5..N: log lines.
- Trailing rows pad to terminal height.

### Status brackets — 8 chars wide

| Token       | Use                          | Color                   |
|-------------|------------------------------|-------------------------|
| `[  OK  ]`  | step succeeded               | `colorDone` bold        |
| `[ FAIL ]`  | step failed (terminal)       | `colorFailed` bold      |
| `[ WARN ]`  | step succeeded with caveat   | `colorNeeds`            |
| `[ SKIP ]`  | step not applicable          | `colorFgMuted`          |
| `[  ..  ]`  | currently running            | cyan/dim, spinner-driven|

Brackets themselves render in `colorFgMuted`; only the inner token carries the
strong color. All four colors come from the existing palette
(`palettes[name]` in `internal/tui/styles.go`), so the splash auto-themes with
`noir` / `paper` / `default`.

### Row format

```
  [  OK  ] <category-18ch>  · <description with optional Japanese>
```

Category is left-padded to a fixed 18-column width so `·` separators
column-align. Description uses east-asian-width-aware spacing via the existing
`github.com/charmbracelet/x/ansi` (`ansi.StringWidth`) — Japanese characters
take 2 cells and that's accounted for.

### Japanese touches

- Header katakana: `レックス` ("Rekkusu") for "Rex".
- Running phrase: `起動中` (kidouchuu, "starting up").
- Done phrase: `準備完了` (junbi-kanryou, "ready").
- A few step descriptions carry Japanese accents — handshake `接続`
  (setsuzoku, "connection"), subscribe `受信中` (jushinchuu, "receiving"),
  audio.load `起動準備` (kidou-junbi, "boot preparation").

### Spinner

Uses the existing `tickSpinner()` Cmd — same animation engine as the board
working spinner. No new ticker.

## Boot steps

The pipeline is 17 visible steps + a synthetic ready footer. Each step
produces one row.

> Numeric values in the example lines below (`8 tools`, `12 cues`, `42
> bindings`, `4 sessions`, pid `4123`, durations, etc.) are illustrative —
> each step computes the real value at runtime.

| #  | name             | what it does                                                | success line                                                     | skip / warn                                                | fail mode |
|----|------------------|-------------------------------------------------------------|------------------------------------------------------------------|------------------------------------------------------------|-----------|
| 1  | `log.init`       | `rexlog.Init("tui")`                                        | `[  OK  ] log.init        · ~/.local/state/rex/tui.log`          | —                                                          | `[ WARN ]` if FS write fails; continue |
| 2  | `paths.ensure`   | mkdir `~/.config/rex`, `~/.local/state/rex`                 | `[  OK  ] paths.ensure    · config + state dirs ok`              | already exist → `[ SKIP ] paths.ensure    · exists`         | `[ FAIL ]` permission denied |
| 3  | `tty.probe`      | width/height, color depth, locale, charset                  | `[  OK  ] tty.probe       · 198×52 · truecolor · ja_JP.UTF-8`    | non-utf8 → `[ WARN ] tty.probe · ascii fallback`            | — |
| 4  | `settings.load`  | parse `~/.config/rex/settings.json`                         | `[  OK  ] settings.load   · ~/.config/rex/settings.json`         | missing → `[ SKIP ] settings.load   · defaults`             | parse err → `[ WARN ]`, fall back |
| 5  | `theme.apply`    | `applyTheme(scheme)` if set                                 | `[  OK  ] theme.apply     · noir`                                | unset → `[ SKIP ] theme.apply     · default`                | — |
| 6  | `audio.init`     | rebuild player with full settings                           | `[  OK  ] audio.init      · soundset=evangelion · vol=0.80`      | muted → `[ SKIP ] audio.init      · muted`                  | — |
| 7  | `audio.load`     | bake PCM for the selected soundset                          | `[  OK  ] audio.load      · 12 cues · 起動準備`                   | muted → `[ SKIP ]`                                          | bake err → `[ WARN ]` fall back |
| 8  | `registry.load`  | parse `internal/registry/builtin.yaml`                      | `[  OK  ] registry.load   · 8 tools (5 enabled · 3 hidden)`      | —                                                          | `[ FAIL ]` malformed yaml |
| 9  | `keymap.bind`    | init key bindings                                           | `[  OK  ] keymap.bind     · 42 bindings`                         | —                                                          | — |
| 10 | `socket.resolve` | resolve daemon socket path                                  | `[  OK  ] socket.resolve  · /tmp/rex-tristan/rex.sock`           | —                                                          | env err → `[ FAIL ]` |
| 11 | `daemon`         | check + spawn `rex-daemon` if needed                        | already-up: `[  OK  ] daemon         · already running (pid 4123)` · spawn: `[  OK  ] daemon         · started · pid 4129 · 312ms` | — | `[ FAIL ]` binary missing / socket timeout |
| 12 | `client.dial`    | `client.Dial(socket)`                                       | `[  OK  ] client.dial     · connected · 8ms`                     | —                                                          | `[ FAIL ]` |
| 13 | `handshake`      | `c.Hello("rex-tui")`                                        | `[  OK  ] handshake       · 接続 · rex-tui`                      | —                                                          | `[ FAIL ]` |
| 14 | `subscribe`      | `c.Subscribe("")`                                           | `[  OK  ] subscribe       · 受信中 · event stream open`          | —                                                          | `[ FAIL ]` |
| 15 | `snapshot.parse` | count sessions by state from snapshot                       | `[  OK  ] snapshot.parse  · 4 sessions · 1 needs · 2 work · 1 done` | empty → `[ SKIP ]  · no sessions yet`                    | — |
| 16 | `state.restore`  | `LoadTUIState()` → selected, filter                         | `[  OK  ] state.restore   · selected=7d4f · filter=all`          | no file → `[ SKIP ] state.restore   · first run`            | — |
| 17 | `renderer.warm`  | precompute lipgloss styles + width caches                   | `[  OK  ] renderer.warm   · styles cached`                       | —                                                          | — |
| ✓  | ready footer     | bottom-anchored, not a row                                  | `準備完了 ready · 1.34s` (total elapsed)                          | —                                                          | — |

### Side effects deferred to hand-off

- `audio.Play(EventStartup)` — the existing rising chime, fires at the moment
  the splash gives way to the board (was previously fired at program start).
- `listenDaemon(m.Client)` — the goroutine that pumps daemon events into
  Bubble Tea, started at hand-off so events from `Subscribe` onwards aren't
  consumed mid-splash.

## Audio per row

### New events

```go
EventBootOK   = "boot_ok"
EventBootWarn = "boot_warn"
EventBootFail = "boot_fail"
// SKIP is silent — no event fired.
// Ready/hand-off reuses the existing EventStartup chime.
```

### Catalog entries

Each soundset's catalog function (`factorioCatalog`, `evangelionCatalog`)
gains entries for the three new events. Sketches:

- **evangelion / `boot_ok`** — single short FM blip (~30 ms, high-mid, NERV
  terminal feel). Quiet enough to cascade cleanly at 70 ms spacing.
- **evangelion / `boot_warn`** — softer, off-tuned blip with a slight tail.
- **evangelion / `boot_fail`** — descending dissonant pair (klaxon edge,
  distinct from `EventDelete`'s pattern-blue motif).
- **factorio / `boot_ok`** — a tight construction-tool click.
- **factorio / `boot_warn`** — a damped thunk.
- **factorio / `boot_fail`** — a dropped-belt clatter.

### Wiring

In `Update`'s `bootStepMsg` branch, after appending the row:

```go
switch msg.Status {
case stepOK:   m.Audio.Play(audio.EventBootOK)
case stepWarn: m.Audio.Play(audio.EventBootWarn)
case stepFail: m.Audio.Play(audio.EventBootFail)
case stepSkip: // silent
}
```

`Player.Play` is fire-and-forget and a no-op when the player isn't enabled,
so muted users hear nothing automatically.

### One bootstrap constraint

For the chime to fire on step 1, the Player has to exist before the first step
msg arrives. So `tui.Run` does a **synchronous pre-bootstrap** before
`tea.NewProgram`:

1. `settings.NewStore() + Load(settings.DefaultPath())` — best-effort. Any
   error (missing file, parse error) is swallowed; the store is left empty
   and defaults apply downstream.
2. Read `sound_enabled` (bool), `soundset` (string), `master_volume` (float64)
   from that store; missing keys default to off / `factorio` / `0` respectively.
3. `audio.New(...)` with those values, attach to `Model.Audio`.

This is invisible — not a log step, no chime, nothing printed.

The splash's `settings.load` step (#4) repeats the load and stores the
*canonical* `Store` on `Model.Store` for the rest of the app. The duplication
is trivial (one JSON read; the file is small) and keeps the splash's log
honest. The splash's `audio.init` step (#6) takes the now-canonical settings
and calls `Player.SetSoundset(name)` to switch the active catalog if needed —
it does *not* rebuild the player. (If `SetSoundset` is insufficient to apply
other config — volume, enabled — we expose lightweight setters on `*Player`
as part of stage 4.)

The new `audio.EventBootOK/Warn/Fail` constants are added to
`internal/audio/audio_test.go`'s `allEvents` slice so the existing
`TestBakeNonEmpty` enforces every soundset (current and future) to define
non-empty PCM for them.

## Transition & race semantics

Two parallel timelines drive the hand-off:

```
Init ─┬─ dispatch step 1 ──→ msg 1 ──→ dispatch step 2 ──→ … ──→ msg 17  [BootDone]
      └─ tea.Tick(1200ms) ───────────────────────────────────→ minMsg    [BootMinDone]
```

After handling any `bootStepMsg` or `bootMinElapsedMsg`, `Update` checks:

```go
if m.BootDone && m.BootMinDone && !m.BootFailed && m.Focus == FocusBoot {
    return m.handOffToBoard()
}
```

`handOffToBoard()`:

1. Set `m.Focus = FocusBoard`.
2. Clear `m.BootLog` (don't keep it in memory once invisible).
3. `m.Audio.Play(EventStartup)`.
4. Return `tea.Batch(tea.HideCursor, listenDaemon(m.Client), tickSpinner())` —
   the Cmds that previously lived in `Model.Init()` for the unsplashed path.

### Three race outcomes

| scenario                                | example                                  | behavior                                                                                  |
|-----------------------------------------|------------------------------------------|-------------------------------------------------------------------------------------------|
| **Boot faster than min** (common)        | steps finish at t=120ms, min at t=1200ms | full pipeline shows, ready footer appears at 120 ms and lingers ~1 s, hand-off at 1200 ms |
| **Boot slower than min** (daemon spawn)  | min at t=1200ms, last step at t=1380ms   | splash shows until 1380 ms; transitions on the final msg                                  |
| **Cold-start worst case**                | `daemon` step → `stepFail` at t=2s       | `BootFailed=true`; transition blocked; failure UI engaged (see below)                     |

### Daemon events arriving during the splash

Step 14 (`subscribe`) succeeds while steps 15–17 are still running. The daemon
may start pushing events on the wire immediately. We deliberately do **not**
start `listenDaemon` until hand-off — those events sit in the socket buffer
(~64 KB) until reading begins. For a ≤1.5 s window this is well within buffer
headroom; if it ever becomes an issue we'd add a passive draining goroutine
that holds events in a slice and replays them post-hand-off. (Not needed for
v1.)

### Window resize during splash

`tea.WindowSizeMsg` is handled the same as in `FocusBoard`: stored on
`m.Width / m.Height`, the splash re-renders with new dimensions on the next
View. No special-casing.

### Keyboard during splash

- `ctrl+c`, `q`, `esc` → quit cleanly (`tea.Quit`). Audio cleanup happens via
  normal program teardown.
- Anything else → ignored. No skip key.

### Constants

```go
const (
    bootMinDuration = 1200 * time.Millisecond
    bootInterStep   = 70  * time.Millisecond
)
```

Both live in a `splash.go` constants block so they're easy to tune.

## Failure behavior

### Two tiers

- **`stepWarn`** — recoverable. Row shown as `[ WARN ]`, audio plays
  `boot_warn`, pipeline **continues** with whatever fallback the step
  established (e.g., settings parse error → defaults).
- **`stepFail`** — terminal. Row shown as `[ FAIL ]`, audio plays `boot_fail`,
  pipeline **halts**. `BootFailed = true` blocks hand-off.

### Which steps can be terminal

| step             | terminal? | example failure                                                |
|------------------|-----------|----------------------------------------------------------------|
| `paths.ensure`   | yes       | permission denied on `~/.config`                                |
| `registry.load`  | yes       | malformed `builtin.yaml`                                       |
| `socket.resolve` | yes       | `XDG_RUNTIME_DIR` unset on Linux + no fallback                  |
| `daemon`         | yes       | `rex-daemon` binary missing / socket timeout                    |
| `client.dial`    | yes       | daemon crashed between spawn and dial                           |
| `handshake`      | yes       | protocol mismatch / server rejection                            |
| `subscribe`      | yes       | server rejection                                               |

All other steps are warn-or-OK; rex can run without any one of them.

### Failure render

```

  ∴ レックス · rex runtime executive
  起動失敗 boot failed

  [  OK  ] log.init        · ~/.local/state/rex/tui.log
  [  OK  ] paths.ensure    · config + state dirs ok
  …
  [  OK  ] socket.resolve  · /tmp/rex-tristan/rex.sock
  [ FAIL ] daemon          · binary not found at /Users/tristan/.local/bin/rex-daemon

    cause: exec: "rex-daemon": executable file not found in $PATH
    fix:   ensure rex-daemon is on PATH or installed alongside rex

                                          press q · ctrl+c to quit
```

- Header swaps `起動中 booting...` for `起動失敗 boot failed` in red.
- Spinner stops; elapsed counter freezes at the failure moment.
- Two lines under the FAIL row: `cause: <err.Error()>` (wrapped) and `fix: <hint>`.
  Hints live in a per-step `map[string]string` keyed by step name; missing
  entries → no fix line.
- Footer becomes the quit prompt.

### Exit path

User presses `q`/`ctrl+c` → `tea.Quit`. `tui.Run` inspects the final model:

```go
final, err := p.Run()
if err != nil { return err }
if fm, ok := final.(Model); ok && fm.BootFailed {
    return fm.BootError  // bubbles to rex main → printed to stderr
}
```

The same error the splash showed lands in stderr scrollback after the
alt-screen tears down, in rex's normal `rex: <err>` format.

### No retry in v1

Easy to add later by binding a key that resets `BootFailed` and re-dispatches
the failing step's Cmd. Out of scope for this milestone.

### Warns

Recorded in the log, never block transition. After hand-off the log is
cleared, so warns are visible only during the splash. They're also written to
`~/.local/state/rex/tui.log` via `rexlog`, so persistent inspection is
possible.

## Testing

### Pure render (snapshot tests) — `internal/tui/splash_snapshot_test.go`

| test                              | state                                                                    | what it locks in                              |
|-----------------------------------|--------------------------------------------------------------------------|-----------------------------------------------|
| `TestSplashInitialFrame`          | `BootStep=0`, empty log, spinner just started                            | header + spinner only                         |
| `TestSplashMidBoot`               | 5 OK rows + 1 active                                                     | column alignment, `[  ..  ]` row              |
| `TestSplashAllOK`                 | full 17-row pipeline, BootDone, BootMinDone (pre-handoff)                | ready footer + full log                       |
| `TestSplashWithWarn`              | one `[ WARN ]` row                                                       | yellow rendering doesn't break alignment      |
| `TestSplashFailed`                | terminal failure at step 11 (`daemon`)                                   | red FAIL + cause + hint + quit prompt         |
| `TestSplashJapaneseColumnAlign`   | rows with `接続` / `受信中` in descriptions                                | east-asian width handled — `·` aligns         |
| `TestSplashNarrowTerminal`        | 60×20 viewport                                                           | text truncation + no panics                   |

### State machine (unit tests) — `internal/tui/splash_test.go`

- `TestBootPipelineAdvances` — `bootStep` increments correctly; `BootDone`
  flips only after the final msg.
- `TestBootHandOffWaitsForMin` — all step msgs at t=0; `BootMinDone=false`,
  no hand-off. `bootMinElapsedMsg` → hand-off batch.
- `TestBootHandOffWaitsForLastStep` — min msg arrives first; hand-off fires
  on last step.
- `TestBootFailureBlocksHandOff` — `stepFail` msg, then min msg; no hand-off,
  `BootFailed=true`, `BootError != nil`.
- `TestBootFailureStopsPipeline` — `Update` doesn't return the next step's Cmd
  after a fail.
- `TestBootHandOffClearsLog` — after hand-off `m.BootLog == nil`, focus is
  `FocusBoard`.

### Audio routing (unit tests) — `internal/tui/splash_audio_test.go`

Uses a recording `audioPlayer` fake (captures every `Play(event)` call).

- `TestSplashChimePerStatus` — each status maps to the right event;
  `stepSkip` produces no call.
- `TestSplashHandOffPlaysStartup` — hand-off batch contains a Play for
  `EventStartup`.
- `TestSplashMutedIsSilent` — fake reports disabled; zero Play calls. (Already
  covered by `audio.Player` muted semantics; this verifies the splash doesn't
  bypass them.)

### Audio catalogs (extend existing test) — `internal/audio/audio_test.go`

`allEvents` gains `EventBootOK`, `EventBootWarn`, `EventBootFail`. Existing
`TestBakeNonEmpty` then forces every soundset (factorio, evangelion, plus any
future addition) to define non-empty PCM for them.

### Step Cmd tests — `internal/tui/splash_steps_test.go`

Most step Cmds are thin wrappers around already-tested code. Two clusters
worth specific tests:

- `TestSnapshotParseCountsByState` — given a synthetic `Snapshot` with mixed
  statuses, the msg's `Desc` reads `N sessions · X needs · Y work · Z done`.
- `TestDaemonStepAlreadyRunning` / `TestDaemonStepSpawnsAndPolls` /
  `TestDaemonStepBinaryMissing` — exercises the three branches against a
  fake socket and a fake `findDaemonBinary`. These tests live in
  `internal/daemonctl/daemon_test.go` since the logic moves there.

### Manual verification (smoke checklist)

- Cadence at 70 ms inter-step delay feels right (not stuttery, not blurred).
- Real audio plays through CoreAudio with the evangelion soundset; chimes
  don't clip each other.
- Terminal returns clean (no leftover alt-screen residue) when failing and
  quitting (`reset` not required).
- A genuinely cold start surfaces `daemon · started · pid X · <ms>` visibly.
  Recipe: `pkill rex-daemon; rex`.
- Color scheme `paper` (light) renders the splash readably — green still
  reads as success, red as failure.

## Files touched & build order

### New files

| path                                          | purpose                                                                                                |
|-----------------------------------------------|--------------------------------------------------------------------------------------------------------|
| `internal/daemonctl/daemon.go`                | extracted from `internal/cli/daemon.go` — `Reachable(socket)`, `Start(socket) (pid, dur, err)`, `FindBinary()`, `DefaultSocket()` |
| `internal/daemonctl/daemon_test.go`           | unit tests for the three branches                                                                       |
| `internal/tui/splash.go`                      | constants, types, `renderSplash(m, w, h)`, `handOffToBoard()`, status-bracket styles, hint map         |
| `internal/tui/splash_steps.go`                | the 17 step Cmds + `bootSequence` slice + `nextStep(m) tea.Cmd`                                         |
| `internal/tui/splash_snapshot_test.go`        | the seven render snapshots                                                                              |
| `internal/tui/splash_test.go`                 | state-machine tests                                                                                     |
| `internal/tui/splash_audio_test.go`           | audio routing tests with recording fake                                                                 |
| `internal/tui/splash_steps_test.go`           | step Cmd tests (`snapshot.parse` parser, etc.)                                                          |

### Modified files

| path                              | change                                                                                                           |
|-----------------------------------|------------------------------------------------------------------------------------------------------------------|
| `internal/cli/tui.go`             | drop `daemonReachable` / `daemonStart` calls; just `tui.Run(socket)`                                              |
| `internal/cli/daemon.go`          | replace inline daemon-spawn with thin wrappers around `daemonctl` (preserves `rex daemon start` CLI behavior)     |
| `internal/tui/tui.go`             | remove pre-`NewProgram` boot work; sync read of audio prefs from `settings.json`; build `Model{Focus: FocusBoot}`; post-run check `BootError` |
| `internal/tui/model.go`           | add `FocusBoot` const; new `Boot*` fields; `audioPlayer` interface                                                |
| `internal/tui/update.go`          | handle `bootStepMsg` / `bootMinElapsedMsg`; dispatch next step; chime; call `handOffToBoard()`                    |
| `internal/tui/styles.go`          | add status-bracket styles (reuse `colorDone` / `colorFailed` / `colorNeeds` / `colorFgMuted`)                     |
| `internal/audio/audio.go`         | add `EventBootOK`, `EventBootWarn`, `EventBootFail` constants                                                     |
| `internal/audio/factorio.go`      | catalog entries for the three new events                                                                          |
| `internal/audio/evangelion.go`    | catalog entries for the three new events                                                                          |
| `internal/audio/audio_test.go`    | extend `allEvents` to include the new boot events                                                                 |

### Build order (5 staged commits)

1. **`internal/daemonctl` extract** — move daemon spawn/check helpers into the
   new package; `cli/daemon.go` delegates. No behavior change. Mergeable
   independently. Has tests.
2. **Audio catalog: boot events** — add `EventBootOK/Warn/Fail` constants +
   factorio + evangelion entries + extend `audio_test.go`. Soundsets are
   richer; nothing triggers them yet. Mergeable independently.
3. **Splash skeleton with stubbed steps** — `FocusBoot`, render, constants,
   hand-off logic. `bootSequence` is hardcoded to 17 fake step msgs on a
   timer. Visible result: typing `rex` shows the splash; transitions to the
   board after 1.2 s. Audio fires per row. Daemon dial still happens
   pre-program. Validates the visual + audio independently.
4. **Real step Cmds + boot-pipeline rewiring** — replace fakes with real Cmds;
   remove pre-`NewProgram` boot work from `tui.Run`; wire `tui.Run` to inspect
   final-model `BootError`. The load-bearing change.
5. **Failure UI + tests** — failure render, hint map, post-failure stderr
   propagation, all unit + snapshot tests.

Stages 1 and 2 are independent. 3 depends on 2. 4 depends on 1 and 3. 5
depends on 4.

## Future work (out of scope for v1)

- A retry key for terminal failures.
- Tailing `daemon.log` inline in the splash on `[ FAIL ] daemon`.
- ASCII-art / branded splash content (the component is swappable; just write a
  new renderer).
- Configurable splash duration via settings.
- Skippable splash via key.
- Per-step verbose flag (`REX_BOOT_VERBOSE=1`) that prints the boot log to
  stderr after hand-off, for debugging cold starts.
