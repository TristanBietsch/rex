# vt10x (rex fork)

Vendored fork of [github.com/hinshun/vt10x](https://github.com/hinshun/vt10x)
at `v0.0.0-20220301184237-5011da428d02` (MIT, see `LICENSE`). Upstream is
unmaintained. rex's `go.mod` points the original import path here with a
`replace` directive, so imports stay `github.com/hinshun/vt10x`.

## Why

Upstream discards every line that scrolls off the top of the screen. rex's
attach popup needs those lines to offer local scrollback for agents that run
on the primary screen (codex inline, ollama, Claude Code without fullscreen).

## Changes from upstream

- The parser state is an enum, not a bound method value, and per-rune
  debug traces only build their arguments when a `DebugLogger` is set.
  Both allocated on almost every byte. See `alloc_test.go`.
- `AttrReverse`, `AttrUnderline`, `AttrBold` and `AttrItalic` export the
  `Glyph.Mode` bits so a renderer can draw them. A reversed cell is stored
  with FG and BG already swapped. See `attr_test.go`.
- `WithScrollback(func(line []Glyph))` option: called with a trimmed copy of
  each primary-screen line just before it is lost — scrolled out of a region
  that starts at the top row (newline/IND at the bottom margin, CSI S), or
  dropped by a resize that shrinks the screen above the cursor. Not called on
  the alternate screen, for regions starting below the top, or for CSI M
  (delete lines). See `scrollback_test.go`.
- CSI sequences with a `<`, `=` or `>` parameter marker are ignored. Upstream
  only recognized `?`, so kitty keyboard sequences (`CSI < u`, `CSI > 5 u`)
  ran DECRC and moved the cursor, and modifyOtherKeys (`CSI > 4;2 m`) reset
  the pen. `CSI ? u`, `CSI ? Pm s` and `CSI ? Pp m` (queries and XTSAVE) no
  longer run DECRC, DECSC or SGR either. See `csi_test.go`.
- Accessors for re-creating a screen elsewhere (rex's daemon repaints its
  screen mirror into attach clients): `View` gains `InactiveCell` (the
  primary screen under an active alternate screen), `SavedCursor` and
  `ScrollRegion`; `AttrBlink`, `AttrGfx`, `AttrWrap`, `CursorWrapNext` and
  `CursorOrigin` export the remaining `Glyph.Mode` and `Cursor.State` bits.
  See `inspect_test.go`.
- `Write` keeps a UTF-8 rune split across two writes and completes it on
  the next one (`PartialRune` returns the held bytes). Upstream dropped the
  rune's first bytes and skipped the rest as invalid, so the rune vanished
  and the rest of its row shifted left. `Write` now always consumes all of
  its input. See `write_test.go`.
- A resize that grows the screen gives the new columns default tab stops
  (every 8). Upstream wrote them into the old slice, so a screen grown from
  120 to 177 columns had no stops past column 119 and a tab from column 112
  jumped to the last column. Stops cleared before the grow stay cleared. See
  `tabs_test.go`.
- `CSI ? 5 W` (DECST8C) resets the tab stops to every 8 columns, which
  ultraviolet (the renderer in Antigravity) sends. See `tabs_test.go`.
- `CSI n b` (REP) repeats the last graphic character `n` times through the
  normal print path (autowrap, pending wrap and the line-drawing charset
  apply). Antigravity's renderer (ultraviolet) uses it for runs of identical
  cells and assumes the cursor moved; ignoring it shifted the rest of the row
  left. The character is forgotten after a control character other than ESC
  and on reset, and `n` is capped at cols*rows so a few bytes cannot make the
  daemon loop 2^31 times. See `rep_test.go`.
- `gofmt` added `//go:build` lines.

Tests here are a separate module: `go -C third_party/vt10x test ./...`
(the Makefile's `test`/`test-race` targets run them).

---

Upstream README:

Package vt10x is a vt10x terminal emulation backend, influenced
largely by st, rxvt, xterm, and iTerm as reference. Use it for terminal
muxing, a terminal emulation frontend, or wherever else you need
terminal emulation.
