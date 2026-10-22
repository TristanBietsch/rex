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

- `WithScrollback(func(line []Glyph))` option: called with a trimmed copy of
  each primary-screen line just before it is lost — scrolled out of a region
  that starts at the top row (newline/IND at the bottom margin, CSI S), or
  dropped by a resize that shrinks the screen above the cursor. Not called on
  the alternate screen, for regions starting below the top, or for CSI M
  (delete lines). See `scrollback_test.go`.
- `gofmt` added `//go:build` lines.

Tests here are a separate module: `go -C third_party/vt10x test ./...`
(the Makefile's `test`/`test-race` targets run them).

---

Upstream README:

Package vt10x is a vt10x terminal emulation backend, influenced
largely by st, rxvt, xterm, and iTerm as reference. Use it for terminal
muxing, a terminal emulation frontend, or wherever else you need
terminal emulation.
