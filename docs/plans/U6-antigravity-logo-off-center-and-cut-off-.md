# U6 Antigravity logo off-center and cut off on the right in the attach popup

## Summary
- Root cause from code reading (no live run yet): rex's vt10x emulator does not implement two things that agy's renderer (charmbracelet/ultraviolet) uses. It ignores REP (`CSI n b`), and it loses tab stops beyond the original width when a screen grows. Every agy session also starts at 120×32 and is then grown to the popup size (138–240 columns in the user's logs). The cursor falls out of step, so the rest of each row is drawn too far left (REP) or jammed at the last column (tabs).
- Fix in vt10x:
  - add REP, with a hard repeat cap;
  - extend tab stops correctly on a resize that grows the screen;
  - honor DECST8C.

  Then spawn agents at the popup size, and never let the popup get wider than the terminal.
- Regression tests use synthetic, ultraviolet-shaped byte fixtures at the vt10x, daemon-repaint and popup-render levels. One live confirmation reuses A1's single D2 agy capture, with a TERM A/B control.

## Current behavior and evidence (file:line)
- **The emulator has no REP.** `third_party/vt10x/csi.go:86-88`: unknown finals go to `unknown`. There is no `case 'b'`, and no `'W'` (DECST8C) either.
- **Resize tab-stop bug.** `third_party/vt10x/state.go:435-443`:
  ```go
  copy(t.tabs, tabs)
  if cols > t.cols {
      i := t.cols - 1
      for i > 0 && !tabs[i] { i-- }
      for i += tabspaces; i < len(tabs); i += tabspaces { tabs[i] = true } // writes the OLD slice
  }
  ```
  After 120→177, `t.tabs` has no stops at columns 120–176. `putTab` (`state.go:308-322`) then moves an HT from column ≥ 112 to the last column. This is an upstream hinshun/vt10x bug, and it has no test (`grep -i tab third_party/vt10x/*_test.go` finds nothing).
- **One cell per rune, no wcwidth** (`parse.go:26-31`). The painter writes one rune per cell (`internal/surface/tui/attach_render.go:109-140`).
- **Spawn size is fixed.** `pty/supervisor.go:125-132` uses 120×32, because `InitialCols`/`InitialRows` are never set (no caller). `protocol.NewSession` has no size (`wire/protocol/intents.go:65-75`). The daemon mirror is created at that size (`supervisor.go:157`) and resized in place (`:159-163`).
- **Logs on this machine.** Every agy spawn logs `pty: started … cols=120 rows=32` (`~/.local/state/rex/daemon.log`). Attach popups open at 138–240 columns (`tui.log`: 177×44, 186×48, 215×55, …). So every first attach grows the PTY, and the mirror's tabs break.
- **Attach order.** `openAttach` sends `Resize` before `Subscribe{Repaint}` (`tui/attach.go:122,126`). Re-attaching at the same size causes no SIGWINCH, so the popup shows the daemon mirror's (corrupted) repaint until agy redraws.
- **agy is a Go binary linking ultraviolet's renderer.** Its symbols include `TerminalRenderer).emitRange`, `xtermCaps`, `TerminalRenderer).SetTabStops` and `RepeatPreviousCharacter`. Logo glyphs found in the binary are `█ ▀ ▄ ░ ▓` (all width 1).
- **What ultraviolet emits** (module cache `ultraviolet@v0.0.0-20260811164956`):
  - `xtermCaps` (`terminal_renderer.go:1677-1725`) gives `tmux*`, `ghostty`, `kitty`, `wezterm`, `xterm-ghostty`/`-kitty` all capabilities, including REP. Plain `xterm-*` drops REP, CHT and HPA.
  - `emitRange` (`:622-690`) uses REP for runs of identical single-byte ASCII cells longer than the REP sequence, for example styled spaces and `-`/`=`/`_` runs, then advances its own cursor.
  - Hard tabs are on when the PTY's `TABDLY == TAB0` (`terminal_screen.go:76-89`, the PTY default), and its tab model is reset on resize (`terminal_renderer.go:1388-1391`).
- **The user's TERM is `tmux-256color`.** A daemon started from tmux passes it to agy (`supervisor.go:122-124`: `os.Environ()` plus extras).

## Ranked hypotheses
1. **H1 (high): REP is dropped.**
   - agy with TERM=tmux* emits `X CSI n b`. vt10x prints one `X` and ignores the rest. ultraviolet assumes the cursor moved n cells, so every later cell on that row is n cells too far left until the next absolute move.
   - Result: an off-center logo and "missing" right-hand cells.
   - It hits the live stream in both emulators (the daemon mirror and the popup), so every attach shows it. A real terminal (`rex attach` from a shell) does not.
2. **H2 (high on re-attach, medium overall): tab stops lost after a grow.**
   - The mirror was created at 120 columns and grown, so an HT past column 112 jumps to the right edge.
   - It shows in repaints (each re-attach) and in the popup after the user enlarges the terminal.
3. **H3 (medium, makes H2 worse): spawn size ≠ popup size.**
   - The first frame is drawn for 120 columns; the first attach always resizes, which triggers H2.
   - Anything agy draws once and never redraws stays laid out for 120 columns.
4. **H4 (low for the logo): no wcwidth.**
   - Wide or emoji glyphs in agy's UI shift later cells by one per glyph, and the painted row gets wider than `cols`. `overlayBox` then widens the popup (`tui.go:232-251`), which clips on near-full-width terminals.
5. **H5 (real bug, not the reported case): popup floor wider than the terminal.**
   - `attachPopupInnerSize` floors `cols` at 40 (`attach.go:209-211`), so popups clip below 54 terminal columns (tmux splits, phones). U5 builds on this fix.
6. **Ruled out:**
   - The repaint itself: it writes rows with autowrap off (`pty/repaint.go:52`).
   - SGR painting (`attach_render.go`).
   - Claude Code: Node/ink sends no REP or HT, which explains why only agy looks wrong.

## Diagnostic procedure
- **A. One minute, by the user, no code:**
  1. Open an agy session in the popup and confirm the logo is off.
  2. In another tmux pane, run `rex attach <sel>`. This shows the same PTY through a real terminal.
  3. If the logo is centered there, the problem is rex's emulator (H1, H2, H4).
  4. `ps -E -p <agy pid> | tr ' ' '\n' | grep ^TERM=` shows the TERM agy got.
- **B. Synthetic (tests, below):** prove H1 and H2 deterministically with no agy.
- **C. Live, one run: piggyback on A1's D2 capture** (wave 5, lane C). Scratch HOME, raw bytes stay in the session scratchpad, never committed.
  1. Daemon with `TERM=tmux-256color`. Spawn agy (120×32), wait for the logo, open the popup at about 177×44, detach, re-attach.
  2. `rex log -bytes <sel>` into a scratchpad file. Count `ESC[<n>b`, `\t`, `ESC[<n>I` and `ESC[?5W`.
  3. Run `REX_U6_CAPTURE=<file> go test ./internal/daemon/pty -run TestU6CaptureReplay -v`. The test is skipped when the variable is unset. It replays the file at 120×32, resizes to the logged attach size, and prints the screen before and after the fix.
  4. Control: repeat with `TERM=xterm-256color`. Expect REP = 0 and a correct logo before the fix if H1 dominates.
  5. Optional: `UV_DEBUG=<scratch>/uv.log` in the daemon environment shows ultraviolet's renderer decisions.

## Design (fix)
1. **vt10x REP** (`csi.go`, `parse.go`, `state.go`).
   - `State.lastc rune` is set after a graphic rune is printed in `parse()`. It stores the pre-gfx rune, so the charset is applied again on repeat. `lastc` is cleared by any C0 control (`handleControlCodes`) and by `reset()`.
   - `case 'b'`: `n := min(max(arg(0,1),1), t.cols*t.rows)`; if `lastc != 0`, print it `n` times through the normal print path, so wrap and pending-wrap follow xterm. `strconv.Atoi` overflow already falls back to the default of 1.
2. **Tab stops on grow** (`state.go:435-443`). Extend `t.tabs` (the new slice), not `tabs`. Start at the last old stop (or 0) and set a stop every 8 up to `cols`. This is st's semantics.
3. **DECST8C** (`CSI ? 5 W`): reset tab stops to every 8.
4. **Spawn at the popup size.**
   - `protocol.NewSession` gains `Cols, Rows uint16` (`omitempty`). An older daemon ignores them.
   - `protocol.ClampTermSize(c, r)` clamps to [20..1000]×[5..500], with 0 meaning the default. It is applied in `validateNewSession` and also in `handleResize`.
   - `spawnSpec` gains `Cols, Rows` and passes them to `SupervisorConfig.InitialCols/Rows`.
   - The TUI (`update.go:551-573` quick-spawn, `wizard.go:317`) sends `attachPopupInnerSize(m.Width, m.Height, attachBannerRows(m))` (S5 signature).
   - `rex new` sends the terminal size when stdout is a TTY.
   - T3's `handleResume` passes the session's last size when known.
5. **Popup never wider than the terminal:** the `cols` floor becomes `min(40, termW-14)`, keeping `attachInnerOrigin` consistent. U5 adds the mobile sheet on top.
6. **Not changed now:** the child's TERM (Q2). vt10x README gains three "Changes from upstream" bullets.

## Files to change
- `third_party/vt10x/csi.go` (`b`, `W`), `parse.go` (`lastc`), `state.go` (resize tabs, reset), new `rep_test.go` and `tabs_test.go`, `README.md`.
- `internal/daemon/pty/repaint_uv_test.go` (fixture; capture-replay helper, env-gated), `supervisor.go` (nothing beyond config use).
- `internal/wire/protocol/intents.go` (`NewSession` size), new `termsize.go` (+ test).
- `internal/daemon/server/validate.go`, `spawn.go` (`spawnSpec` and the `startSupervisor` config), `handlers.go:168-180` (clamp).
- `internal/surface/tui/update.go`, `wizard.go`, `attach.go:196-216`, `attach_mouse.go`, new `attach_uv_test.go`.
- `internal/surface/cli/session/new.go`.
- `docs/protocol.md`, `docs/tui.md`, CHANGELOG.

## Test plan (test-first; each fails before its fix)
- **vt10x (separate module, run by `make test`):**
  - `TestREPRepeatsLastGraphic`: `ab\x1b[3bc` → `abbbbc`, cursor at x = 6.
  - `TestREPAfterControlIsNoop` (`a\r\x1b[3b`).
  - `TestREPWrapsWithAutowrapAndClipsWithout`.
  - `TestREPCountCapped`: `\x1b[2147483647b` finishes in under 50 ms and the screen is full.
  - `TestREPUsesGfxCharset`.
  - `TestResizeGrowExtendsTabStops`: 120 → 177, then `\r` + 15×`\t` + `X` puts X at column 120 (today: 176).
  - `TestResizeShrinkThenGrowTabs`.
  - `TestTBCThenGrow`.
  - `TestDECST8CResetsTabs`.
- **Fixture:** `uvLogoFrame(width)` is a hand-written generator. It emits what ultraviolet does for a centered 5-line block logo: `ESC[H ESC[2J`, CUP per row, truecolor SGR per cell, `█▀▄`, ECH for long blank runs, `' ' CSI n b` for short styled-space runs, and HT moves.
  - The expected screen is the same logo laid out by plain string math.
- **pty:**
  - `TestUVLogo_MirrorAfterGrowRepaints`: mirror at 120×32, Resize 177×44, write the frame at 177, `Repaint` into a fresh vt10x 177×44. Every row equals the expected row.
  - `TestUVLogo_LiveStream`: no resize.
  - `TestU6CaptureReplay`: skipped unless `REX_U6_CAPTURE` is set.
- **TUI:**
  - `TestAttachView_UVLogoCentered`: feed the fixture into `AttachState.Term`, `renderAttach`, strip ANSI. The logo's left and right margins differ by at most 1, and no line is wider than `cols`.
  - `TestAttachPopupNeverWiderThanTerminal` for w in 30..60 (H5).
- **Spawn size:**
  - `TestNewSessionSizeClamped`: protocol and validate tests.
  - `TestSpawnUsesRequestedSize`: testutil daemon; the `pty: started` attributes or the mirror size equal the request; 0 gives 120×32.
  - `TestQuickSpawnSendsPopupSize`: recorder spawner.
  - `TestResizeClamped`: 65535×65535 is clamped and no huge allocation happens.

## Docs/CHANGELOG
- `third_party/vt10x/README.md`: the REP, tab-on-grow and DECST8C changes.
- `docs/protocol.md`: `NewSession.cols/rows` and the clamp on `Resize`.
- `docs/tui.md` ATTACH: agents start at the popup size; the popup is clamped to the screen.
- CHANGELOG Fixed:
  - "Attach popup draws Antigravity (and other ultraviolet/Bubble Tea v2 agents under tmux/ghostty/kitty TERM) correctly: logo centered, no cut-off rows."
  - "Agents start at the popup size."
  - "Daemon clamps terminal sizes."

## Conflict map and placement
- **A1:** shares the D2 capture (wave 5, lane C). The REP fix also corrects agy screen-mirror text (last_line and summarizer input) that S1 and A1's fixtures depend on. Tell S1.
- **S5:** U6-4 calls `attachPopupInnerSize(w, h, bannerRows)`, so it lands after S5-1. U6-5 changes the same function's floor, so coordinate with S5-1 and U5-5 (U5-5 builds the sheet on U6-5).
- **P1/P2:** additive `NewSession` fields (`Name`, `Skills`, `TrustFolder`); trivial rebase.
- **T3:** `handleResume` passes `Cols/Rows` into `spawnSpec`.
- **U4:** it changes `m.Width`, so the spawn size follows automatically.
- **Placement:**
  - U6-1..3 (vt10x and fixtures; no overlap with anything): wave 3, **lane B**, after A1-2. Any idle builder can take it earlier.
  - U6-4, U6-5: wave 5, **lane B**, after S5-1.
  - Live confirmation: wave 5, **lane C**, inside the A1 capture step.

## Security notes
- The daemon's vt10x mirror parses untrusted agent output. An uncapped REP would let an agent (or a prompt-injected agent) loop about 2³¹ cell writes inside the daemon reader, so the cap is cols×rows.
- **Existing finding:** `handleResize` passes any `uint16` size to `screen.Resize` (`server.go:487-495` → `supervisor.go:159-163`). A same-user client asking for 65535×65535 makes vt10x allocate about 4.3 G cells per screen, enough to OOM the daemon. U6-4 adds `ClampTermSize` to both `Resize` and `NewSession`.
- Raw agy captures stay in the scratchpad and are never committed (D2). Fixtures are synthetic.
- No change to child env or argv (Q2 default).

## Open questions for user
1. **Live confirmation?**
   - (a) Fold it into A1's single D2 agy capture (same run, TERM A/B, bytes stay in the scratchpad). **[recommended]**
   - (b) A separate dedicated capture run.
   - (c) No capture: ship the fixture-proven fixes and you eyeball the result.
2. **TERM given to agents?**
   - (a) Leave the inherited TERM; fix vt10x only (REP, tabs, DECST8C). **[recommended]**
   - (b) Also set `TERM=xterm-256color` + `COLORTERM=truecolor` for every agent, to match what vt10x emulates.
   - (c) Set TERM only for antigravity, through a registry `env` field.
3. **Spawn size?**
   - (a) The TUI sends its popup size in `NewSession`, so the first attach needs no resize. **[recommended]**
   - (b) The daemon remembers the last attach size and uses it for new spawns.
   - (c) Keep 120×32.
4. **Fixture source?**
   - (a) Hand-written ultraviolet-shaped fixtures only. **[recommended]**
   - (b) Also a dev-only generator module (its own `go.mod` under `dev/`) that renders through real ultraviolet.
   - (c) A test-only ultraviolet dependency in the main `go.mod`.

## Ordered steps (each one commit; `make check` green)
1. `fix(vt10x): extend tab stops when a resize grows the screen; honor DECST8C`: tests, README.
2. `feat(vt10x): REP repeats the last graphic character (capped)`: tests, README.
3. `test(attach): ultraviolet-shaped logo stays centered through repaint and resize`
   - pty and TUI fixtures, plus the env-gated capture replay.
   - CHANGELOG Fixed (the attach rendering line).
4. `feat(spawn): start agents at the TUI popup size; clamp terminal sizes`
   - Protocol, validate, `spawnSpec`, TUI, wizard, `rex new`, `Resize` clamp.
   - protocol.md, tui.md, CHANGELOG.
5. `fix(tui): attach popup never wider than the terminal`: floor clamp, origin tests.
6. *(After the wave-5 capture, only if it shows something new, such as H4 wide glyphs.)* Follow-up fix plus a fixture for that finding.

## Size estimate
**S–M.** 5 small commits, about 400–600 lines, mostly tests. vt10x wcwidth support (H4) is out of scope unless the capture shows it.

### Critical Files for Implementation
- /Users/tristan/Documents/personal/dev/rex/third_party/vt10x/state.go (resize tab bug :435-443) and /Users/tristan/Documents/personal/dev/rex/third_party/vt10x/csi.go (REP/DECST8C)
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/board.go (rowCols/layoutRows/boardSlots/renderRow; U5)
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/tui.go and /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/update.go (View wrap, WindowSizeMsg/MouseMsg choke points; U4)
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/attach.go plus attach_mouse.go (popup geometry, spawn size, clamp; U5/U6)
- /Users/tristan/Documents/personal/dev/rex/internal/catalog/settings/registry.go plus the new internal/catalog/theme/edge.go (U4/U5 settings, edge palette)

**Read-only checks I ran:**
- `go build ./...` and the vt10x tests pass.
- `go version -m` and a symbol grep on `~/.local/bin/agy` (it links ultraviolet's `emitRange`, `xtermCaps` and `RepeatPreviousCharacter`).
- The rex logs show the 120×32 spawn vs 138–240-column attach sizes.
- `~/.local/share/rex/sessions` has no agy transcripts left, so the live capture is still needed to confirm H1 vs H2.
- The orchestration file changed during my run. I took its current content as given: U3 dropped (D13), U4–U6 still "plan pending". No files were edited.
