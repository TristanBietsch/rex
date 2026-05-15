# Plan D smoke test

Manual end-to-end verification of the polish layer.

## Setup

```sh
make build-all
rm -f /tmp/rex.sock; rm -rf /tmp/rex-state ~/.config/rex/config.yaml
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state &
DAEMON=$!
sleep 0.5
export REX_SOCKET=/tmp/rex.sock
```

For TUI tests below, point the daemon at the default socket (`$XDG_RUNTIME_DIR/rex.sock`) or override with `--socket`.

## 1. SIGHUP reload

```sh
# Add a new tool to user config
mkdir -p ~/.config/rex
cat > ~/.config/rex/tools.yaml <<'YAML'
tools:
  - id: aider
    name: "aider"
    category: paid
    command: ["aider"]
    detect:
      kind: heuristic
      prompt_regex: "^> $"
      idle_ms: 1500
    icon: "◐"
    color: "#F472B6"
    models:
      - id: gpt-5.2
        name: "gpt-5.2"
        args: ["--model", "gpt-5.2"]
YAML

# Send SIGHUP and confirm aider appears in the wizard list
./rex reload
# (Open the TUI, press n — aider should appear)
```

## 2. Modal input forwarding

- Open TUI: `./rex`
- Spawn an echo `prompt` session: press `n`, pick Echo / "Waits for input", confirm.
- Once it lands in **Needs input**, press `enter` on it.
- Type `hello from me` and press enter.
- The agent should print `got: hello from me` and complete. Press `esc` to detach.

## 3. 5-step wizard

- Press `n`. Pick a provider (Claude or similar with effort options) → enter.
- Pick a model → enter.
- Step 3 (effort) appears for models with effort options. Use j/k, enter.
- Step 4 (name): tab cycles slug / title / cwd. Edit, enter.
- Step 5: confirm summary, enter launches.

## 4. `rex config` round-trip

```sh
./rex config list | head -20
./rex config set spinner moon
./rex config get spinner          # → moon
./rex config reset spinner
./rex config get spinner          # → braille
```

## 5. TUI settings page

- Open TUI: `./rex`
- Press `S`. Settings page appears.
- j/k to a Bool row (e.g. "Done blink"). Press enter — value toggles.
- j/k to the Spinner row. Press enter — cycles through braille / ascii_line / moon / pulse / blocks.
- Press `r` to reset the current row. Press esc — page closes and config.yaml is saved.

## 6. Slash palette

- In TUI: press `/`. Palette opens.
- Type `set` — filters to `/settings`. Enter — settings page opens.
- Esc back. `/`. Type `find dark` enter — board filters to rows mentioning "dark".
- `/` empty enter — runs `/find` with empty query (clears filter).

## 7. Audio

- Quit and restart the TUI. You should hear the startup chime.
- Spawn a session — `create` tone.
- Wait for it to complete — `done` tone.
- Press `enter` on a row — `open` tone. Esc — `close` tone.
- Hold `j` / `k` — tactile `nav` click stream.
- `./rex config set sound_enabled false` — mutes everything.

## Cleanup

```sh
./rex daemon stop || kill $DAEMON
rm -rf /tmp/rex-state /tmp/rex.sock
```

## Acceptance

Plan D is **done** when steps 1–7 all work and `make test` is green with the race detector clean.
