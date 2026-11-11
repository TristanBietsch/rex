# S5 Attach banner: a one-row summary above the agent's screen

## Summary
- The in-TUI attach popup gets one banner row between its title bar and the rule above the agent's PTY. It shows what the session is doing (S3 description, falling back to the task), where it is (short cwd, working/waiting/ended time, description age) and, when S4 is on, the state of the other sessions.
- When the text overflows, `attach_banner: marquee` (the default) scrolls it as a ticker. `static` cuts it at a word boundary, `off` hides it, and `reduce_motion` forces static. `::banner` hides or shows it for the current run.
- The PTY is sized to the rows that remain, and resized whenever the banner appears or disappears. Mouse origin, scrollback and the repaint all go through one chrome-rows value. The standalone `rex attach` CLI gets no banner.

## Current behavior (file:line)
- **Popup sizing.**
  - `attachPopupInnerSize(termW, termH)` (`tui/attach.go:196-216`) computes `cols = termW-14`, `rows = termH-12`. The 12 rows are border+padding (4), title, rule, rule and footer (4), and the outer margin (4). Floors are 40×8.
  - `openAttach` (`:99-177`) sizes the PTY **before** Subscribe (`c.Resize`, `:122`), so the daemon's repaint has the popup's geometry.
  - `resizeAttach` (`:218-232`) runs on `tea.WindowSizeMsg` (`update.go:71-75`). It resizes vt10x, clamps `ScrollOff`, then `Client.Resize`.
- **Render.** `renderAttach` (`:435-466`) draws the title bar, the rule, `rows` × `attachViewRow`, the rule, then the footer or command bar. `attachTitleBar` (`:468`) already shows the state chip, name, tool · model and geometry.
- **Mouse.** `attachInnerOrigin` (`attach_mouse.go:48-67`) hard-codes `popupH = rows+4+2+2` and origin `oy+1+1+2` (border, padding, title, rule). `TestAttachInnerOrigin_MatchesRenderedPopup` (`attach_mouse_test.go:50`) pins it to the rendered View.
- **Scrollback.** `attach_scroll.go` pages by `a.Rows` (`:186-187`). A shrink pushes rows into scrollback; `resizeAttach` clamps the offset.
- **Animation.**
  - The spinner chain is 100 ms and runs only while something works (`events.go:61-93`). It is armed at the `Update` choke point (`armSpinner`, `update.go:50`).
  - The description animation is `registerDescAnim` (`model.go:381`) with `descTickMsg`. It respects `desc_animation` and `reduce_motion`.
  - `m.Clock` overrides `now` in tests.
- **Attach commands.** `executeAttachCommand` (`attach_cmd.go:139-163`) handles `q/done/kill/name`; anything else detaches and runs as a board command. The settings page can't be opened while attached (`::settings` closes the popup).
- **CLI `rex attach`** (`cli/session/attach.go`) is a raw passthrough. Agent bytes go straight to the user's real terminal (alt-screen, absolute cursor addressing) with the PTY sized to the full terminal (`:83-88`, `:113-118`).
- **Tests bypass `Client`.** Tests build `AttachState` without a `Client` (`attach_mouse_test.go:38-47`, `attach_bench_test.go:24-40`). `resizeAttach` would nil-deref, so a seam is needed.

## Design

### 1. One source of truth for chrome rows
- `AttachState.BannerRows int` (0 or 1) is the banner the popup shows **now**; geometry derives from it.
- `attachPopupInnerSize(termW, termH, bannerRows int)` subtracts `bannerRows` from `rows`. The floor stays 8.
- `attachBannerRows(m Model) int` returns 1 when all of these hold:
  - the mode is not `off`;
  - `!m.bannerHidden`;
  - the agent keeps at least 12 rows (`attachPopupInnerSize(w,h,0)` gives rows ≥ 13, which means the terminal is at least 25 rows tall).

  On short terminals the banner gives way, so the agent never drops to the 8-row floor because of it.
- `openAttach` computes `banner := attachBannerRows(m)` **before** the first `c.Resize`, so the repaint and the agent's first frame already have the final size.
- `resizeAttach` recomputes `banner` together with cols and rows. A change in any of them resizes vt10x, clamps `ScrollOff`, and sends one `Resize`.
- A new seam, `AttachState.resize func(cols, rows uint16) error`, defaults to `Client.Resize` and is replaced by a recorder in tests.
- `attachInnerOrigin`: `popupH = rows + 4 + a.BannerRows + 2 + 2`; origin `y = oy + 1 + 1 + 2 + a.BannerRows`.

### 2. Placement
Title bar → **banner** → rule → PTY rows → rule → footer. The banner belongs to rex's chrome above the rule, so the agent's screen never looks taller than it is. A click or wheel on the banner row falls outside `[0, Rows)` and is ignored, as on the title bar.

### 3. Content (`bannerParts(m, sum) (lead string, meta []string)`; every piece goes through `protocol.FlattenDisplayText`)
- **lead:** `sum.Description` (S1–S3). If empty, `task: ` + title. If that is empty too, the question line (`last_line` ending in `?`). Otherwise empty.
- **meta**, in order:
  - short cwd: `~` for HOME, then the last 2 path elements;
  - time: `working 12m` (`WorkingSince`), `waiting 3m` (`LastEventAt`), or `ended 14:02` (T2 `ended_at`, falling back to `LastEventAt`);
  - `updated 40s ago` (S1 `description_at`; muted when older than 2 min; left out when absent);
  - when S4's `overview_enabled` is on: `elsewhere: 2 to review · 1 needs you`, from `overview.Group(m.Sessions)` excluding this session, followed by S4's headline when present.
- **static:** `↳ ` + `truncateWords(lead, avail)` (S3) on the left, the first two meta items right-aligned; the meta is dropped below 60 columns. While a `DescAnim` for this session is active, the lead cell shows `renderAnimFrame`, which reuses `desc_animation`.
- **marquee:** one plain string, `↳ lead  ·  task: title (only when lead is the description)  ·  meta…`. If it fits in `cols`, it renders exactly like static and does not tick. Otherwise it scrolls.

### 4. Marquee mechanics (pure, time-based, so tests are deterministic with `m.Clock`)
- `marqueeWindow(text string, width int, elapsed time.Duration) string`:
  - pause `bannerPause = 2s` at offset 0;
  - then 1 cell per `bannerStep = 120ms` over `text + "   ·   " + text`;
  - slice with `ansi.Cut` on cell columns and pad to exactly `width` cells. A wide rune cut at either edge becomes a space.
- The loop restarts (`a.BannerSince = now`) when the popup opens and when this session's `description` changes. That happens in `applyEvent` next to the existing `registerDescAnim` call, so a new summary is always read from its start.
- Tick chain: `bannerTickMsg{Gen}`, with `bannerGen`/`bannerRunning` on Model. It is armed at the `Update` choke point by `armBanner`, a sibling of `armSpinner`, while `wantsBannerTick(m)` holds:
  - the popup is visible (FocusAttach, or FocusCommand with Attach);
  - `BannerRows == 1`;
  - the mode is marquee and `reduce_motion` is off;
  - `!a.Ended`;
  - the text overflows.

  The chain stops on detach, during the screensaver, and when the text fits. When nothing scrolls, there is no extra CPU.
- The whole marquee line uses one style (`styleDim`) except a fixed `↳ ` prefix (colorWorking). That avoids span bookkeeping inside a scrolling window; U1 can restyle.

### 5. Toggles
- **Setting** (`registry.go`, appended after S4's `overview_enabled`):
  ```go
  { ID: "attach_banner", Label: "Attach banner", Section: SectionSummary, Type: TypeEnum, Default: "marquee",
    Options: []string{"marquee", "static", "off"},
    Help: "Row above the agent in the attach popup: what it is doing, where, and (with overview) what the rest are doing. reduce_motion makes marquee static." },
  ```
  It is read when a popup opens and on every resize.
- **`::banner`** in `executeAttachCommand` flips `m.bannerHidden` for this TUI run, calls `resizeAttach`, and sets a Notice (`banner hidden — ::banner shows it`). The `renderAttachCommandBar` hint gains `banner`.

### 6. Effect on the agent
- Every agent sees a terminal one row shorter, a normal SIGWINCH.
- Full-screen agents (Claude Code, Codex, agy) redraw at the new size. The daemon mirror follows `Resize`, so later repaints match.
- On primary-screen agents, a toggle that shrinks the view pushes the top row into rex's scrollback; growing does not pull it back. This is the documented no-reflow behavior.
- Another client attached at a different size (a CLI `rex attach`) still fights over the size, as it does today.

### 7. CLI `rex attach`: no banner
- The agent draws on the user's real terminal with absolute cursor positions. Reserving a row needs either a scroll region with origin mode, which agents and rex's own repaint reset (`\x1b[r`), or running the output through vt10x, which turns the CLI into the TUI popup.
- Users who want the banner use the TUI popup. A later sanitized OSC 2 window title is an option (open question 3).

### 8. KindMarquee (reserved by S2) stays unused
The banner is built from fields that already exist, so it adds no Ollama load. The S3 `long` detail already carries progress and next steps.

## Files to change
| File | Function / region | New/Mod |
|---|---|---|
| `internal/surface/tui/attach.go` | `AttachState` (+`BannerRows`, `BannerSince`, `resize`), `openAttach`, `attachPopupInnerSize` (+param), `resizeAttach`, `renderAttach` (insert banner line), comment at `:203` | mod |
| `internal/surface/tui/attach_banner.go` | `attachBannerRows`, `bannerMode`, `bannerParts`, `renderAttachBanner`, `marqueeWindow`, `shortCWD`, `bannerTickMsg`, `armBanner`, `wantsBannerTick` | new |
| `internal/surface/tui/attach_mouse.go` | `attachInnerOrigin` | mod |
| `internal/surface/tui/attach_cmd.go` | `executeAttachCommand` (`banner`), `renderAttachCommandBar` hint | mod |
| `internal/surface/tui/update.go` | `Update` (arm banner), `dispatch` `case bannerTickMsg` | mod |
| `internal/surface/tui/model.go` | `Model` (+`bannerHidden`, `bannerGen`, `bannerRunning`); `applyEvent` SessionUpdated (reset `BannerSince` for the attached session) | mod |
| `internal/surface/tui/help.go` | Attached-session group: `::banner` | mod |
| `internal/catalog/settings/registry.go` | `attach_banner` | mod |
| Tests: `attach_mouse_test.go` `attachWithTerm`, `attach_scroll_test.go:23`, `attach_bench_test.go:37` | pass `bannerRows` (mechanical) | mod |

## Test plan (test first)
- **Geometry:**
  - `TestAttachPopupInnerSize_BannerTakesOneRow`.
  - Extend `TestAttachInnerOrigin_MatchesRenderedPopup` to banner on and off at 120×40, 101×33, 200×60.
  - `TestAttachBanner_HiddenOnShortTerminal` (24 rows → 0, 25 → 1).
- **PTY sizing:**
  - `TestOpenAttach_SizesPTYForBanner`: extend `attach_e2e_test.go` with a testutil daemon; the session receives `Resize(cols, rows)` with `rows = termH-13`, read from the daemon mirror size or a stub resizer.
  - `TestAttachCommand_BannerToggleResizes`: the recorder sees rows+1, then rows-1; `ScrollOff` stays ≤ `sb.Len()`; vt10x is resized.
- **Content:**
  - `TestBannerParts` fallbacks: description → task → question → cwd/time only.
  - `TestBanner_SanitizesUntrusted`: a description or title with `\x1b]0;evil\x07`, `\x1b[2J`, bidi overrides and newlines renders as one clean row of exactly `cols` cells.
  - `TestBanner_ElsewhereExcludesSelf` (S4 on).
- **Marquee:**
  - `TestMarqueeWindow` table: elapsed 0 / 1.9 s (offset 0), 2 s + 5 × 120 ms (offset 5), wraparound, CJK at both edges, exact width every frame.
  - `TestBanner_FitsMeansStaticNoTick`.
  - `TestBanner_ReduceMotionStatic`.
  - `TestBannerTick_ChainStopsOnDetachAndSaver`.
  - `TestBanner_RestartsOnNewDescription`.
- **Mouse and scroll:**
  - Click on the banner row → no input sent.
  - Wheel on the first PTY row → `cy == 0` (SGR report `;1M`).
  - Scroll mode works after a toggle.
- **Snapshots** (`snapshot_test.go`, fixed `m.Clock`, 120×40): `attach-banner-static`, `attach-banner-marquee-frame`, `attach-banner-off`. Every line is ≤ width, and the View is exactly `Height` lines.
- **Bench:** `BenchmarkAttachView_BannerMarquee` next to the existing attach benches. The frame time must stay within about 10 % of no-banner; record it in the PR.
- **Registry:** `attach_banner` enum with default marquee; an invalid value is rejected.

## Docs/CHANGELOG
- `docs/tui.md` ATTACH: new `### Banner` subsection covering:
  - content and order;
  - marquee, static and off;
  - `reduce_motion`;
  - hidden below 25 rows;
  - `::banner` (and the command-bar table row);
  - the agent's terminal being one row shorter while it shows.
- Fix the mouse and scrollback paragraphs if wording mentions sizes.
- `docs/settings.md` AI summary table: `attach_banner`.
- `docs/cli.md` / `docs/tui.md`: note that `rex attach` from a shell has no banner.
- `docs/agents/modules/tui.md`: `attach_banner.go`.
- CHANGELOG `### Added`: "Attach popup banner above the agent: what it is doing, where, for how long, and (with the overview) what the other sessions need; scrolls when long (`attach_banner`: marquee/static/off, `::banner` hides it)."

## Conflict map
- **S3:** reuses `truncateWords` and `renderAnimFrame`. Per S3's note it does not mount the preview line in the popup. S3's `boardBottomRows(m)` is untouched.
- **S4:** the "elsewhere" segment needs `overview.Group` and `Model.Overview` (S4 steps 2 and 6). Without them, step 4 below is skipped and the banner shows this session only.
- **T1/T5/header:** none. S5 does not touch `renderHeader`, `headerHeight` or `headerHitAt`; T1's note assumed a board-header marquee, which this design avoids.
- **T2:** `ended_at` for "ended 14:02", falling back to `LastEventAt`. **S1:** `description_at` for "updated 40s ago"; the segment is left out when absent.
- **model.go `applyEvent`:** a one-line hunk next to `registerDescAnim`. S1, T2 and T3 touch `applyPatch`, not this block.
- **update.go `Update`:** `armBanner` goes next to `armSpinner`/`armIdle`. T1 and T5 edit other cases.
- **attach*.go:** U3 may move `altScreenTools`, a different region. A1 touches neither file. U1 restyles the banner through palette styles; no colors are hard-coded.
- **registry.go:** appended after S4's entry.

## Security notes
- **Untrusted inputs.** The banner shows the model description (an injection-capable paraphrase of agent output), the agent's question line (raw screen text), the title, name and cwd, and S4's headline.
- **Sanitizing.** Every piece goes through `FlattenDisplayText` before composition, the marquee slices plain text by cells, and nothing is written as OSC or title. A hostile agent can at most show misleading text in one dim row, and it already controls its own screen.
- **Not spoofable from the agent's screen.** The banner sits above rex's rule, outside the agent's rows, so the agent cannot draw into it.
- **Mouse input.** Banner rows never forward input; hit-testing is pinned by the origin test.
- **No new data leaves the machine.** The banner adds no Ollama calls.

## Open questions for user
1. Default `attach_banner`? **Recommended: `marquee`** (as asked), with `reduce_motion` making it static. The alternative is `static` by default.
2. Should this session's own status lead, or the board-wide overview? **Recommended: this session first, then "elsewhere" counts and S4's headline when the overview is on.**
3. Should a shell `rex attach` show anything? **Recommended: no banner.** An optional later step is putting the description in the window title (OSC 2, sanitized) behind a flag.
4. Should `::banner` persist to config.yaml? **Recommended: no, this run only.** The setting is the persistent switch.
5. Use S2's reserved `KindMarquee` for a longer model-written "story so far"? **Recommended: not now.** Build from existing fields, with no extra Ollama load.

## Ordered steps (each one commit; `make check` passes)
1. `refactor(tui): attach chrome rows and resize from one place`: `bannerRows` param (always 0), `BannerRows`, the `resize` seam, `attachInnerOrigin`; test edits are mechanical. No behavior change.
2. `feat(tui): static attach banner and attach_banner setting`: setting, `bannerParts`, `renderAttachBanner`, sizing on open and resize, mouse/origin/sanitize tests, static snapshots.
3. `feat(tui): marquee scrolling for long attach banners`: `marqueeWindow`, tick chain, `reduce_motion`, restart on a new description, bench.
4. `feat(tui): ::banner hides or shows the banner and resizes the agent`.
5. `feat(tui): banner shows what the other sessions need` (after S4 step 6).
6. `docs: attach banner and CHANGELOG` (raise the TUI floor if it rose).

## Size estimate
M

### Critical Files for Implementation
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/attach.go (plus attach_mouse.go, attach_cmd.go, new attach_banner.go)
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/header.go (plus keymap.go `headerHeight`, board_mouse.go `headerHitAt`)
- /Users/tristan/Documents/personal/dev/rex/internal/daemon/adapter/claude_hooks.go (plus pty/supervisor.go poll site)
- /Users/tristan/Documents/personal/dev/rex/internal/daemon/server/server.go (plus client.go `subscribeBoard`, handlers.go `handleHello`) and /Users/tristan/Documents/personal/dev/rex/internal/wire/protocol/events.go
- /Users/tristan/Documents/personal/dev/rex/cmd/rex-daemon/main.go and /Users/tristan/Documents/personal/dev/rex/internal/catalog/settings/registry.go

### Findings that change scope
- **"Ready for review" doesn't exist for Claude today.** Stop, Notification and SessionStart all map to `needs_input` (`claude_hooks.go:30-41`). S4 needs the small `wait_reason` hook change: Claude Code 2.1.286 on this machine sends `notification_type` (`permission_prompt`, `idle_prompt`, …). The adapter package has a 98 % coverage floor, so that step needs full branch tests.
- **"Fleet" is already a user-facing word** (`rex fleet`, `SessionSummary.Fleet`). I recommend calling the feature "overview" and keeping `KindFleet` internal.
- **SummarizerHealth isn't in the Snapshot**, so a TUI that connects late misses it. S4's overview is in the Snapshot to avoid the same gap.
- **The banner is attach-only.** T1's assumption that S5 touches the board header no longer applies, and `headerHitAt` needs no change for S5.
- **`resizeAttach` needs a test seam.** It calls `m.Attach.Client.Resize` directly, and tests build `AttachState` without a client, so S5 step 1 adds `AttachState.resize`.
- `go build ./...` passes and the current attach geometry tests pass, so the step-1 refactor starts from a green baseline.
