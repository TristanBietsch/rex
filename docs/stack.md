# Stack

charmbracelet/bubbletea — Elm architecture
charmbracelet/lipgloss — layout and styling
charmbracelet/bubbles — viewport, list, spinner (skip textinput)
charmbracelet/glamour — markdown rendering

Concurrency

golang.org/x/sync/errgroup — structured supervision
golang.org/x/sync/semaphore — fan-out limits
context.Context on every boundary

Audio

hajimehoshi/oto — cross-platform, no cgo
(Sound effect when creating, job done, and deletion, etc..)

Error handling

stdlib fmt.Errorf with %w. No pkg/errors.

Engineering constraints
NASA Power of Ten. Bounded loops. No unbounded recursion. Every return checked. Functions short (~60 LOC ceiling). Two assertions per function on average. Smallest possible data lifetimes. Clean lint with a strict config.
Unix philosophy. One process, one job. Text streams. Debuggable with stock tools (socat, jq, tail, lsof).
