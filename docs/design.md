# Design

Minimal, monospace, Unix-flavored. Color is **only** used for status. Tool branding colors appear **only** in the new-agent wizard and the session modal's top strip — never on the board.

## Palette

### Background and chrome

| Token | Hex | Role |
| --- | --- | --- |
| `bg.base` | `#0F1115` | Terminal background |
| `bg.elev` | `#171922` | Selected row, modal backdrop |
| `bg.modal` | `#1B1E29` | Modal panel |
| `fg.primary` | `#E6E6E6` | Default text, slugs, section headers |
| `fg.dim` | `#7A7F8C` | Description, elapsed time, help line |
| `fg.muted` | `#4A4F5A` | Inactive chip, separator |
| `border.subtle` | `#262A36` | Modal and section dividers |

### State palette (the only color on the board)

| State | Glyph | Hex | Role |
| --- | --- | --- | --- |
| working | spinner (TBD) | `#5B8DEF` | active session |
| needs_input | `◆` | `#E5B341` | blocking on user |
| done | `●` | `#4ADE80` | completed cleanly |
| failed | `✕` | `#EF4444` | exited non-zero or unrecoverable adapter error |
| crashed | `○` | `#7A7F8C` | from a prior daemon run; live PTY gone |

Blue working, yellow needs-attention, green done, red failed, dim crashed. These are the only colored glyphs on the board.

### Tool palette (wizard and modal top-strip only)

Pulled from each model's brand. Used to tint the tool's monogram and name during selection; **never** appears in a board row.

| Tool | Monogram | Hex | Source |
| --- | --- | --- | --- |
| claude | `◆` | `#D97757` | Anthropic coral |
| codex | `◇` | `#10A37F` | OpenAI green |
| gemini | `◈` | `#4285F4` | Google blue |
| ollama | `◉` | `#B8A382` | warm cream (llama) |
| grok | `⬢` | `#E5544D` | xAI accent red |
| deepseek | `⊙` | `#5E72E4` | DeepSeek indigo |
| kimi | `◗` | `#7C3AED` | Moonshot purple |

The two palettes are deliberately disjoint in temperature, but they also live on disjoint screens, so they never collide on a single view.

## Typography

- Monospace, terminal-controlled. Rex makes no assumption about which font is loaded.
- Three weights via Lipgloss styles: regular (default), bold (slug + section header), dim (description + help line).
- No italics — monospace italic coverage across terminals is poor.

## Spacing

- Section header followed by one blank line.
- One blank line between sections.
- Row internals: `state (1.4c)` `id (5c, 4-char hex)` `slug (22c, left-padded)` `description (left, ellipsis)` `model·effort (18c, dim)` `time (5c, right)`.
- Modal: 2-cell internal padding on all sides.

## Animations

All durations under ~1.5 s, all gated by `--no-animation` and a TTY check.

| Name | Trigger | Style | Duration |
| --- | --- | --- | --- |
| Spinner | Row state is `working` | see *Spinner* below | continuous |
| Section slide | Row's section changes | Ease-out translate Y | 200 ms |
| Header tick | Aggregate count changes | Vertical wipe | 150 ms |
| Modal scale-in | Modal opens | Scale 0.96 → 1.00 with opacity | 120 ms |
| Row pulse | Row newly created | Single background fade in tool color | 250 ms |
| **Done blink** | Row transitions to `done` | Four cycles of background flash with `state.done` at 35 % alpha, then back to normal | ~1.6 s |

The blink fires once on the transition (paired with the `done` audio tone) and the row returns to its normal appearance afterwards. No persistent tint.

Motion confirms state changes; it doesn't decorate them.

## Spinner

Single style across all working rows: **braille dots**.

| Frames | Cadence |
| --- | --- |
| `⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏` | ~10 fps |

Falls back to a static `*` when `--no-animation` or `--ascii` is set, or when stdout is not a TTY.

## Audio

**Sound design: Factorio-inspired.** Short, crisp, mechanical. Every tone has a sharp attack and an exponential-decay envelope (`amp(t) = e^(-t/τ)`) so the synth reads as *inserter clicks*, *assembler chirps*, and *deconstruction thunks* rather than musical beeps. Higher pitches for small UI events (nav, modal panels), lower for state transitions (delete, deconstruction-style). Multi-tone bursts for important moments (create, done) to imitate the layered samples Factorio uses for completion feedback.

All tones are pre-baked PCM at daemon startup; the runtime just hands buffers to `oto`. No JIT synthesis on the audio hot path.

| Event | When | Tones (freq Hz × dur ms, decay τ ms) | Vibe |
| --- | --- | --- | --- |
| `startup` | `rex` TUI launches (or reattaches) | `220×60 τ=30` → `440×60 τ=30` → `880×100 τ=60` with `1760×40 τ=20` octave overlay on the third | Factory coming online — confident, ascending |
| `nav` | Selection moves (`j`/`k`/`↑`/`↓`/scroll) | `1200×10 τ=6` | Inserter tick — tiny crisp click |
| `open` | Modal opens (enter / double-click) | `330×30 τ=18`, then `660×30 τ=18` | Panel slide-open |
| `close` | Modal closes (esc / click out) | `660×30 τ=18`, then `330×30 τ=18` | Panel slide-close |
| `create` | New session launched | `220×30 τ=20` → `440×40 τ=25` → `660×50 τ=30` | Assembler spool-up, three ascending bursts |
| `done` | Session transitions to `done` | `880×80 τ=70` with a `1760×40 τ=20` octave overlay on the attack | Research-complete ding, slightly metallic |
| `delete` | Session deleted | `330×40 τ=25` → `165×40 τ=30` | Deconstruction thunk, descending |

**Design notes.**

- The decay envelope is the most important part — a sine wave at constant amplitude sounds musical; the same sine wave with a 20 ms decay sounds like a mechanical click. That's the trick that gets us 80 % of the Factorio character without bundling samples.
- `nav` is tiny (10 ms total) so holding `j` produces tactile click-click-click rather than overlapping smears.
- `open`/`close` are inverse two-tone slides — the ear hears "up" and "down" without conscious effort.
- `done`'s octave overlay (a brief tone an octave above the fundamental) is what gives it the slight metallic resonance of Factorio's completion sounds.
- When no audio device is available: ANSI BEL (`\a`) once per event. `nav` is auto-disabled in BEL fallback so rapid scrolling doesn't spam.

User overrides: drop `~/.local/share/rex/audio/{startup,create,done,delete,nav,open,close}.wav` to replace any synthesized tone with a file (e.g. an actual Factorio rip if you own the game and want maximum authenticity). Per-event settings live in `~/.config/rex/config.yaml`:

```yaml
audio:
  enabled: true
  startup: { tone: { freq: [220, 440, 880, 1760], dur_ms: [60, 60, 100, 40], decay_ms: [30, 30, 60, 20], overlay_index: 3 } }
  create:  { tone: { freq: [220, 440, 660],       dur_ms: [30, 40, 50],     decay_ms: [20, 25, 30] } }
  done:    { tone: { freq: [880, 1760],           dur_ms: [80, 40],         decay_ms: [70, 20], overlay_index: 1 } }
  delete:  { tone: { freq: [330, 165],            dur_ms: [40, 40],         decay_ms: [25, 30] } }
  nav:     { enabled: true, tone: { freq: [1200], dur_ms: [10],             decay_ms: [6] } }
  open:    { tone: { freq: [330, 660],            dur_ms: [30, 30],         decay_ms: [18, 18] } }
  close:   { tone: { freq: [660, 330],            dur_ms: [30, 30],         decay_ms: [18, 18] } }
```

(`overlay_index` selects which tone in the sequence is layered on top of its predecessor as an octave overlay, rather than played sequentially after it.)

Setting any event to `enabled: false` mutes that event without disabling the rest. `nav` is the most likely candidate to mute for users who find frequent ticks distracting.

## Iconography rules

1. **One glyph per board row** — the state marker. No tool monogram on the board.
2. **Tool monograms appear only** in the new-agent wizard (during provider/model selection) and in the modal's top strip (so you know what's running while you're driving it).
3. **Color = status**. Tool colors are scoped to wizard/modal context.
4. Every glyph has an ASCII fallback used when `TERM=dumb` or `--ascii` is set: spinner → `*`; `◆` → `!`; `●` → `+`; `✕` → `x`; `○` → `o`; tool monograms in the wizard → uppercase first letter in brackets (`[C] [X] [G] [O]`).
6. **Prompt indicators.** `λ` precedes the new-session prompt (and the modal reply line). `:` precedes the command-mode prompt and is tinted with `state.needs` yellow so command mode is unmistakable. The send glyph at the right side of the λ prompt is `›` and only appears when the prompt is non-empty.
5. No Nerd Font glyphs in v0. Terminal portability over typography.
