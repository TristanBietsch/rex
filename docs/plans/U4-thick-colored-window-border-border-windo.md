# U4 Thick colored window border (`:border`, `window_border*` settings)

## Summary
- rex draws its own thick frame around the whole TUI screen. The terminal screen size is kept apart from the layout size, so every view (board, overlays, attach popup) sits inside the frame without any per-view code.
- The colors are 10 Pantone Color-of-the-Year hues, each tuned to sit next to an Okabe–Ito role. Tests check them for contrast (at least 3:1 against every U1 background) and for colorblind separation (Machado-2009 simulations, OKLab ΔE). A smaller "random pool" is the set whose colors stay far apart.
- `:border [on|off|next|random|<name>|#hex]` changes the frame for this run. The settings `window_border`, `window_border_color` (default `random`) and `window_border_style` make it permanent. The picker is a reusable `theme.PickEdge` for the R06 workspace edge colors.

## Current behavior (file:line)
- No frame. `View` renders at `m.Width × m.Height` taken directly from `tea.WindowSizeMsg` (`internal/surface/tui/update.go:71-75`, `tui.go:72-114`).
- Every layout reads `m.Width/m.Height`:
  - board row budget: `keymap.go:164-183` (`boardTop`, `boardHeightFor`, `boardHeight`);
  - attach PTY geometry: `attach.go:110,196-232`;
  - attach mouse origin: `attach_mouse.go:48-67`;
  - header chip hit-test: `board_mouse.go:156-161`;
  - settings height: `settings.go:518,653`;
  - screensaver: `tui.go:87` → `screensaver.go:177-203`;
  - splash: `tui.go:91`.
- Mouse events arrive in screen coordinates and go straight to `updateMouse` (`update.go:262-267`).
- Colors:
  - `palettes` hold only the default/noir/paper hexes (`styles.go:20-62`).
  - `fleetPalette` / `fleetColor(name)` already hashes a name to a color (`board.go:152-171`). This is the same idea R06 needs.
- Settings Appearance block: `internal/catalog/settings/registry.go:10-53`. Commands are a `switch` (`command.go:17-110`) until U1-7's table lands.
- `rex render` builds a `Model` with `Width/Height` set directly and no WindowSizeMsg (`cli/inspect/render.go:42-51`).
- The user runs tmux (`TERM=tmux-256color`). Escape-sequence tab or background colors would not reach the outer terminal without DCS passthrough.

## Design
**1. What "border around the current rex window" means.** Three options:

| Option | Where it shows | Verdict |
|---|---|---|
| TUI-drawn frame | Every terminal: tmux, SSH, phone | **Recommended.** Testable with goldens. Costs 2 cols + 2 rows. |
| Terminal escape codes (iTerm2 `OSC 6` tab color, OSC 11 background, WezTerm user vars) | Some terminals only | Invisible inside tmux. Must be restored on crash. Untestable. Possible later opt-in (iTerm2 tab tint). |
| tmux pane border (`select-pane -P`) | tmux only | Changes the user's tmux state. Out of scope. |

**2. Geometry, with one choke point.**
- `Model` gains `TermW, TermH` (raw terminal size). `Width/Height` keep their meaning: the layout area, now inside the frame.
- New `frame.go`:
  - `frameOn(m)` is true when all of these hold:
    - `window_border` is on (or toggled on for this run);
    - `TermW > 0` (so `rex render` and tests that set `Width` directly never get a frame);
    - `Focus != FocusBoot` (the splash stays full-bleed; the frame appears at `handOffToBoard`);
    - `!m.saverOn` (Q5);
    - `TermW-2 ≥ 48 && TermH-2 ≥ 14` (tiny terminals and phones: the frame gives way).
  - `relayout(m)` sets `Width = TermW - 2*inset`, `Height = TermH - 2*inset`, then calls `resizeAttach(m)` and `ensureVisible`. It is called from `WindowSizeMsg`, from `:border`, from the settings live-apply, and on screensaver on/off.
- `View()`: the existing body becomes `viewBody(m)`. If `frameOn`, `wrapFrame(body)` pads or truncates each body line to `Width` (measured once with `ansi.StringWidth`) and adds the side glyphs. The top and bottom rows are cached by `(TermW, color, style, profile)`, so a frame adds only string concatenation.
- Mouse: in `dispatch` `case tea.MouseMsg`, while `frameOn`:
  - events on frame cells are dropped (they still count as input for the idle timer);
  - other events get `X--, Y--` before `updateMouse`.

  Board rows, chips (`headerHitAt`'s `y-1`), settings and attach (`attachInnerOrigin`) then work unchanged, because they all use `Width/Height`.
- **Views:**
  - Overlays (help, settings, wizard, stats, fail, confirm) center inside the inner area automatically.
  - Attach: the PTY becomes 2 cols and 2 rows smaller. A toggle resizes the agent (a normal SIGWINCH) through S5's `resize` seam.
  - Screensaver and splash: no frame.
  - CLI `rex attach`: no frame (raw passthrough).

**3. Glyphs (`window_border_style`).**
- `thick` (default): sides `█` (1 cell wide); top `▀` and bottom `▄` (half a cell tall, which is about one cell-width on a 1:2 cell); corners `█`. All four edges look equally thick.
- `line`: heavy box drawing `┏━┓┃┗┛`.
- Glyphs are drawn in the foreground color only, so the terminal background shows through.

**4. Palette (`internal/catalog/theme/edge.go`, the U1 package).**
- Each entry is `EdgeColor{ID, Ref, Dark, Light, ANSI string}`.
- Method:
  - **Contrast:** WCAG 2.x SC 1.4.11 non-text contrast, at least 3:1 against the theme background. `Dark` is used when `Palette.Dark`, `Light` on paper. The `ansi` theme uses the ANSI index.
  - **Colorblind separation:** Machado/Oliveira/Fernandes 2009 severity-1 protan/deutan/tritan matrices in linear sRGB, plus normal vision, with OKLab ΔE×100 between every pool pair. Okabe–Ito scores 7.6 under this metric.
  - **Anchors:** Okabe & Ito "Color Universal Design", Paul Tol's palettes and the IBM CVD-safe set. Each Pantone hue sits next to one of their roles.
- Contrast was computed with the WCAG formula against `#0F1115` (U1 default/classic) and `#FBFAF7` (paper). Against noir `#05080F` and high-contrast `#000000`, every value is higher still.

| id | Pantone reference (Color of the Year) | Dark hex | vs `#0F1115` | Light hex | vs paper | Okabe–Ito role |
|---|---|---|---|---|---|---|
| tangerine | 17-1463 Tangerine Tango | #DD4124 | 4.37 | #DD4124 | 4.14 | vermillion |
| mimosa | 14-0848 Mimosa | #F0C05A | 11.15 | #B18513 | 3.23 | orange |
| illuminating | 13-0647 Illuminating | #F5DF4D | 13.99 | #A18C00 | 3.21 | yellow |
| emerald | 17-5641 Emerald | #009473 | 4.93 | #009473 | 3.67 | bluish green |
| cerulean | 15-4020 Cerulean | #9BB7D4 | 9.10 | #758FA9 | 3.21 | sky blue |
| classic-blue | 19-4052 Classic Blue | #4F83C0 (lightened) | 4.81 | #0F4C81 | 8.49 | blue |
| very-peri | 17-3938 Very Peri | #6667AB | 3.68 | #6667AB | 4.92 | (IBM indigo) |
| orchid | 18-3224 Radiant Orchid | #B163A3 | 4.66 | #B163A3 | 3.88 | reddish purple |
| viva-magenta | 18-1750 Viva Magenta | #BE3455 | 3.43 | #BE3455 | 5.28 | (IBM magenta) |
| ultimate-gray | 17-5104 Ultimate Gray | #939597 | 6.29 | #8A8C8E | 3.23 | neutral |

- **Random pool (computed):**
  - Dark themes use 7 colors: tangerine, illuminating, cerulean, very-peri, viva-magenta, mimosa, ultimate-gray. Worst-case ΔE is 7.0 (illuminating vs mimosa under tritan), about Okabe–Ito's own 7.6.
  - Light themes use 5 colors: mimosa, cerulean, very-peri, viva-magenta, classic-blue. Worst case is 9.8. On paper, yellow cannot reach 3:1 and also stay distinct from mimosa.
  - All 10 together fall to 2.9 (emerald vs classic-blue), so the rest are only offered by name.
- `PickEdge(seed string, dark bool, avoid []string) EdgeColor` uses `pool[fnv1a(seed) % n]` and skips colors in `avoid`. `ParseEdge(s)` accepts an id or `#rgb`/`#rrggbb`, lowercases it and rejects everything else. **R06 reuse:** `PickEdge(workspaceID, dark, usedColors)` gives the "random unused swatch", and R04's `pantone_safe` theme can read `EdgeColors`.

**5. "Random" (Q2, recommended: stable per launch folder).**
- The seed is the TUI's start folder (git root if inside a repo), so the same project keeps its color.
- `:border next` moves to the next pool color for this run.
- When R06 lands, the seed becomes the workspace id and the color comes from `workspace.json`.

**6. Settings** (Appearance, right after `header_style`, contiguous):
- `window_border` (bool, default off per Q1).
- `window_border_color` (TypeString, default `random`). Its Options are `random` plus the 10 ids, so ←/→ cycles. It is validated by `Normalize: ParseEdge`.
- `window_border_style` (enum `thick|line`, default `thick`).
- Settings changes apply live through `relayout`. U2's SettingsHub `applyLive` must list these 3 ids.

**7. Command** (a row in U1-7's command table, so help picks it up):
- `:border` with no argument toggles the frame for this run.
- `:border on|off|next|random|<id>|#hex` sets it for this run and shows a notice, e.g. `border: tangerine (random · :border next)`.
- `::border` works in the attach popup without detaching (sibling of S5's `::banner`).
- Not persisted (Q6).

**8. NO_COLOR / ansi.**
- Under the Ascii profile (NO_COLOR) the frame is still drawn, in monochrome, and `:border` notes that NO_COLOR is set.
- Under the `ansi` theme it uses `EdgeColor.ANSI`: tangerine 9, mimosa 3, illuminating 11, emerald 2, cerulean 14, classic-blue 12, very-peri 13, orchid 5, viva-magenta 1, ultimate-gray 8.
- On 256-color terminals lipgloss downsamples. A test checks that the pool still maps to distinct ANSI256 indices.

## Files to change
- New:
  - `internal/catalog/theme/edge.go`, `edge_test.go`, `cvd_test.go` (Machado matrices and OKLab, test-only);
  - `internal/surface/tui/frame.go`, `frame_test.go`.
- Modified:
  - `internal/surface/tui/model.go` (`TermW`, `TermH`, `borderOverride`, `borderColor`);
  - `update.go` (`WindowSizeMsg` → `relayout`; `MouseMsg` translation);
  - `tui.go` (`View` → `viewBody` + `wrapFrame`);
  - `splash.go` (`handOffToBoard` calls `relayout`);
  - `screensaver.go` (`relayout` on saver on/off);
  - `settings.go` (live-apply of the 3 ids);
  - `keys.go` / `command.go` (`:border` row);
  - `attach_cmd.go` (`border`);
  - `internal/catalog/settings/registry.go` (3 rows; `registry_test` section-contiguity).
- Tests touched: `attach_mouse_test.go` (origin with frame).
- `tools/coverage/baseline.txt` (theme floor).

## Test plan (test-first)
- **theme:**
  - `TestEdgeContrast`: every Dark variant ≥ 3.0 against default/classic/noir/high-contrast Bg; every Light variant ≥ 3.0 against paper.
  - `TestEdgePoolCVDSeparation`: dark pool ≥ 6.5, light pool ≥ 6.5 under none/protan/deutan/tritan.
  - `TestEdgePoolANSI256Distinct`.
  - `TestPickEdgeDeterministicAndAvoid`.
  - `TestParseEdge`: ids, `#ABC`, case; rejects `\x1b[31m`, `#12`, `rgb()`, a 65-byte string.
- **TUI:**
  - `TestFrameGeometry`: WindowSizeMsg 120×40 gives `Width` 118 and `Height` 38. `View` has 40 lines, each exactly 120 cells, with `█▀▄` at the right cells.
  - `TestFrameMouseTranslation`: a click at (x+1, y+1) with the frame selects the same row as (x, y) without it. Clicks on the frame are ignored.
  - `TestAttachInnerOrigin_MatchesRenderedPopup` extended to frame on/off. `TestFrameToggleResizesAttach` uses the resize recorder (S5 seam).
  - `TestFrameHiddenOnSplashSaverAndTinyTerminals`.
  - `TestFrameNoColorMonochrome`: Ascii profile, no SGR, glyphs present.
  - `TestBorderCommand`: `:border`, `tangerine`, `#ff00ff`, `next`, `off`; `nope` gives an error naming the valid forms.
  - `TestFrameEveryView`: board, help, settings, wizard, stats, fail, confirm, attach. Each is exactly `TermH` lines of `TermW` cells.
  - `TestRenderCommandNoFrame` (`TermW == 0`).
- **Goldens** (U1 harness): `board-frame-80x24` plain and styled; `attach-frame-120x40`.
- **Bench:** `BenchmarkAttachView_Frame` within about 5% of no-frame (record it in the commit body).

## Docs/CHANGELOG
- `docs/tui.md`: a new "Window border" section (what, the command, auto-hide rules, attach is 2×2 smaller) and a command-table row.
- `docs/settings.md`: Appearance rows, the palette table, the method note ("Pantone-inspired; hex values are approximations; not affiliated").
- `docs/howto/customize-appearance.md`.
- `docs/agents/modules/tui.md` (`frame.go`) and the theme module doc (edge palette, R06 hook).
- CHANGELOG Added: "Optional thick colored window border (`:border`, `window_border*`), Pantone-inspired colorblind-safe colors, random per project."

## Conflict map and placement
- **U1** (hard prerequisite): needs `catalog/theme` (U1-3), `Palette.Dark`, the command table (U1-7, already planned early) and the golden harness (U1-1).
- **U2:** registry rows are next to `theme`. SettingsHub live reload must call `relayout` for `window_border*`. Merge U4 before U2-9, or U2-9 adds the ids.
- **S5:**
  - `attachPopupInnerSize(w, h, bannerRows)` takes the inner size, so there is no conflict.
  - `::border` sits next to `::banner` in `executeAttachCommand`.
  - It reuses S5's `AttachState.resize` seam.
- **U5:** `layoutClass` reads the inner `Width` (after the frame). Both hide chrome on tiny terminals; the frame's 48-column floor sits below U5's mobile breakpoint (≤ 60), so a frame can still show in mobile.
- **T1/S4:** `headerHitAt` and `headerHeight` are unaffected thanks to translation and the inner size.
- **Screensaver:** relayout on toggle.
- **R04/R06:** consumers of `EdgeColors` / `PickEdge`.
- **Placement:** wave 7, **lane B** (a second TUI builder; lanes B and C are idle in waves 6–7), in parallel with U2 in lane A, after U1 has fully merged. It must not run during wave 6 (U1 is a single builder touching every TUI file).

## Security notes
- `window_border_color` and `:border <arg>` are user strings. Only palette ids or a validated `#rrggbb` ever reach `lipgloss.Color`, so there is no escape injection through config or a Lua overlay (U2).
- The frame is built from constant glyphs. No OSC or terminal-state writes, so nothing needs restoring if the process crashes.
- Mouse events on the frame are dropped and never forwarded to an agent PTY.

## Open questions for user
1. **When is the border shown?**
   - (a) Off by default; `:border` turns it on for this run, and settings make it permanent. **[recommended]**
   - (b) On by default for everyone, in a random color.
   - (c) On by default only while two or more rex TUIs are connected.
2. **What does "random" mean?**
   - (a) Stable per launch folder (same project, same color; `:border next` re-rolls for the run). **[recommended]**
   - (b) A new random color on every launch.
   - (c) Random per launch, avoiding colors other open rex windows use (the daemon tracks them).
3. **How thick?**
   - (a) Block frame: `█` sides, `▀`/`▄` top and bottom; looks even, costs 2 cols + 2 rows. **[recommended]**
   - (b) Extra-thick: 2-cell sides and full-cell top and bottom rows (costs 4 cols + 2 rows).
   - (c) Heavy box-drawing line `┏━┓┃` only.
4. **Palette naming?**
   - (a) Pantone Color-of-the-Year names as ids (`tangerine`, `classic-blue`, …), with docs saying "Pantone-inspired". **[recommended]**
   - (b) Plain ids (`orange`, `blue`, …), with Pantone references only in docs.
   - (c) Official Pantone codes as ids (`17-1463`). This is a trademark risk.
5. **Screensaver and splash?**
   - (a) Hide the frame on both (OLED burn-in, minimal look). **[recommended]**
   - (b) Keep the frame everywhere.
   - (c) Keep it, but dimmed, on the screensaver.
6. **Does `:border` persist?**
   - (a) This run only; the settings page persists. **[recommended]**
   - (b) `:border` writes config.yaml.
   - (c) This run only, plus `:border save`.

## Ordered steps (each one commit; `make check` green)
1. `feat(theme): Pantone-inspired colorblind-safe edge palette`
   - `edge.go`: table, pools, `PickEdge`, `ParseEdge`.
   - Contrast, CVD and ANSI256 tests; coverage floor.
2. `refactor(tui): separate terminal size from layout size`
   - `TermW/TermH`, `relayout`, `viewBody`/`wrapFrame` with the inset always 0, the mouse-translation hook.
   - Goldens stay byte-identical.
3. `feat(tui): thick colored window border and window_border settings`
   - 3 registry rows, the frame renderer and cache, auto-hide rules, NO_COLOR/ansi handling, attach resize on toggle, live apply.
   - Tests, goldens regenerated (named in the commit body), bench; tui.md, settings.md, CHANGELOG.
4. `feat(tui): :border command and ::border in the attach popup`
   - Table row, `attach_cmd`, notices, tests, docs.

## Size estimate
**M.** 4 commits, about 600–800 lines including tests and goldens.
