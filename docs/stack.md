# Stack

Rex is a Go-only system. No cgo. No Python. No build-time codegen. Two binaries, one socket, one YAML file.

## TUI

| Library | Purpose |
| --- | --- |
| `github.com/charmbracelet/bubbletea` | Elm-architecture event loop driving the TUI |
| `github.com/charmbracelet/lipgloss` | Layout and styling (colors, borders, padding) |
| `github.com/charmbracelet/bubbles` | Re-usable components: `viewport`, `list`, `spinner` |
| `github.com/charmbracelet/glamour` | Markdown rendering — stocked for v1 modal markdown, **not used in v0** |

The TUI does **not** use `textinput` from `bubbles`. The bottom prompt and wizard fields are custom — one input model, not two.

## Concurrency

| Library | Purpose |
| --- | --- |
| `golang.org/x/sync/errgroup` | Structured supervision per session and per daemon subsystem |
| `golang.org/x/sync/semaphore` | Fan-out limit on live PTYs (`max_concurrent_sessions`, default 16) |
| `context.Context` (stdlib) | Carried on every blocking boundary |

Every goroutine has a parent. Every parent has a cancellation handle. No detached goroutines.

## PTY

| Library | Purpose |
| --- | --- |
| `github.com/creack/pty` | Spawn child processes attached to a pseudo-terminal; cross-platform, no cgo |

## Audio

| Library | Purpose |
| --- | --- |
| `github.com/hajimehoshi/oto/v2` | Cross-platform audio output, no cgo on macOS/Linux/Windows |

v0 **synthesizes** sine-wave tones in-process. No WAV assets ship with the binary. Three events: `create`, `done`, `delete`. Fallback to ANSI BEL when no audio device is available. Users can drop files into `~/.local/share/rex/audio/` to override the synthesized tones.

## IPC and serialization

| Library | Purpose |
| --- | --- |
| `net` (stdlib) | Unix domain socket |
| `encoding/json` (stdlib) | Newline-delimited JSON on the wire |
| `gopkg.in/yaml.v3` | Parse `~/.config/rex/tools.yaml` and `~/.config/rex/config.yaml` |

JSON-lines for machines, YAML for humans. Debuggable with `socat`, `jq`, `cat`, `tail`, `lsof`.

## Scripting / configuration

| Library | Purpose |
| --- | --- |
| `github.com/yuin/gopher-lua` | Lua 5.1 interpreter, pure Go, no cgo |

Used for `~/.config/rex/init.lua` — the optional Lua config layer that overlays the YAML config. Powers the settings page's persistence path (when Lua is the source of truth), custom keybinds, and user-defined `:foo` commands. See `settings.md` for the Lua surface.

## Error handling

- `fmt.Errorf("...: %w", err)` for wrapping. No `pkg/errors`.
- No panics outside `main`.
- Every returned `error` is checked.

## Engineering constraints

**NASA Power of Ten** (applied as guidance, not religion):

1. Bounded loops, no unbounded recursion.
2. Functions short — soft ceiling of ~60 LOC per function.
3. Smallest possible data lifetimes; prefer values over pointers when small.
4. Check every returned `error`.
5. Two assertions per function on average (`if cond { return fmt.Errorf(...) }` counts).
6. No dynamic allocation in hot paths after startup (per-session ring buffers sized once).
7. Each function does one thing; multi-purpose functions are decomposed.
8. Avoid pointer-to-pointer-to-pointer indirection.
9. Compile under a strict lint config (`golangci-lint` with `errcheck`, `govet`, `staticcheck`, `revive`).
10. No build-tag matrix; one binary, one target per OS.

**Unix philosophy:**

- One process, one job. `rex` renders, `rex-daemon` supervises.
- Text streams on the wire. Newline-delimited JSON for IPC, YAML for config.
- Debuggable with stock tools: `socat`, `jq`, `cat`, `tail`, `lsof`, `kill -HUP`.
- Composability: `rex ls --json | jq` should always work.
