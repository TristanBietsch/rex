# Plan C smoke test

Manual end-to-end check of the TUI.

## Setup

```sh
make build-all
rm -f /tmp/rex.sock; rm -rf /tmp/rex-state
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state &
export REX_SOCKET=/tmp/rex.sock
```

(You'll need to point the TUI at `/tmp/rex.sock` either via `XDG_RUNTIME_DIR` or by killing the default daemon. For local dev, the default socket at `$XDG_RUNTIME_DIR/rex.sock` is fine — just run `./rex-daemon &` with no args.)

## 1. Open the TUI

```sh
./rex
```

Expect: `∴ REX` header, three sections (Needs input / Working / Completed), each currently `(none)`, λ prompt at the bottom, help bar showing keybind hints.

## 2. Spawn via λ prompt

- Press `i` — cursor enters the λ prompt.
- Type `hello world` and press enter.
- A new session appears under Working (the echo tool's short script), spinner ticks.
- Within a few seconds, it migrates to Completed with a green blink animation.

## 3. Spawn via wizard

- Press `n` — wizard opens. Select Echo with enter. Select a model with enter. Confirm with enter.
- A new session appears.

## 4. Selection + jump

- `j` / `k` moves selection.
- `1` / `2` / `3` jumps between sections.
- `g` / `G` go to top / bottom.
- Watch the spinner tick on Working rows.

## 5. Filter

- Press `t` to cycle the filter chip. Only sessions for the selected tool show.

## 6. Delete

- Select a session. Press `d` then `d` (chord). Session disappears.

## 7. Modal

- Select a Working session. Press `enter` — modal opens with PTY output.
- Press `esc` — back to the board.

## 8. Help overlay

- Press `?` — help overlay opens with four titled sections.
- Press `esc` — back to the board.

## 9. Command mode + quit

- Press `:` to enter command mode. Type `q` and enter.
- Confirm prompt appears. Press `n` — back to board.
- Press `:` again, type `q!` and enter — quits immediately.

## 10. Background + reattach

- Open TUI again. Select a running session. Press `t` to set a non-`all` filter.
- Press `:`, type `bg`, enter.
- TUI exits. Run `rex` again — board reopens with same selection and filter restored.

## 11. Mouse

- Click on a row — selection moves to it.
- Double-click on a Working row — modal opens.
- Press `esc` — back to the board.

## Acceptance

Plan C is **done** when steps 1–11 all work and `make test` is green with the race detector clean.
