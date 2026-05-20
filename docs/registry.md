# Tool and model registry

Rex discovers everything spawnable through a YAML registry. Built-in defaults ship in the binary; user overrides merge on top from `~/.config/rex/tools.yaml`. **Adding a new model never requires Go code** — a YAML entry is enough.

## Merge rules

1. The built-in registry loads first.
2. `~/.config/rex/tools.yaml` loads second.
3. For each top-level tool entry, user entries *override* built-ins by `id`.
4. Within a tool, `models` from the user file *extend* the built-in models list (also keyed by `id`).

This lets you add a model to a built-in tool without redeclaring the whole tool.

## Built-in defaults

```yaml
tools:
  - id: claude
    name: "Claude Code"
    category: paid
    command: ["claude"]
    detect:
      kind: structured
      format: claude_jsonl
    icon: "◆"
    color: "#D97757"                # Anthropic coral; appears only in wizard + modal
    models:
      - id: opus
        name: "Opus 4.7"
        args: ["--model", "opus"]
        effort:
          options: [minimal, default, high, max]
          default: default
          arg_template: "--effort={value}"
      - id: sonnet
        name: "Sonnet 4.6"
        args: ["--model", "sonnet"]
        effort:
          options: [minimal, default, high, max]
          default: default
          arg_template: "--effort={value}"
      - id: haiku
        name: "Haiku 4.5"
        args: ["--model", "haiku"]
        effort:
          options: [minimal, default]
          default: default
          arg_template: "--effort={value}"

  - id: codex
    name: "OpenAI Codex"
    category: paid
    command: ["codex"]
    detect:
      kind: heuristic
      prompt_regex: "^❯ "
      idle_ms: 1200
    icon: "◇"
    color: "#10A37F"                # OpenAI green; appears only in wizard + modal
    models:
      - id: gpt-5
        name: "GPT-5"
        args: ["--model", "gpt-5"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--reasoning-effort={value}"
      - id: gpt-5-codex
        name: "GPT-5 Codex"
        args: ["--model", "gpt-5-codex"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--reasoning-effort={value}"

  - id: gemini
    name: "Gemini CLI"
    category: paid
    command: ["gemini"]
    detect:
      kind: heuristic
      prompt_regex: "^> "
      idle_ms: 1200
    icon: "◈"
    color: "#4285F4"                # Google blue; appears only in wizard + modal
    models:
      - id: 2.5-pro
        name: "Gemini 2.5 Pro"
        args: ["--model", "gemini-2.5-pro"]
      - id: 2.5-flash
        name: "Gemini 2.5 Flash"
        args: ["--model", "gemini-2.5-flash"]

  - id: ollama
    name: "Ollama (local)"
    category: self_hosted
    command: ["ollama", "run"]
    detect:
      kind: heuristic
      prompt_regex: ">>> $"
      idle_ms: 1500
    icon: "◉"
    color: "#B8A382"                # warm cream (llama); appears only in wizard + modal
    models:
      - id: llama3.1
        name: "llama3.1"
        args: ["llama3.1"]
      - id: mistral
        name: "mistral"
        args: ["mistral"]
      - id: custom
        name: "Custom (prompt)"
        args_prompt: "model tag"

  - id: grok
    name: "Grok (xAI)"
    category: paid
    command: ["grok"]
    detect:
      kind: heuristic
      prompt_regex: "^> $"
      idle_ms: 1200
    icon: "⬢"
    color: "#E5544D"                # xAI accent red; wizard + modal only
    enabled_by_default: false       # opt-in via Settings → Onboarding → models
    models:
      - id: grok-4
        name: "Grok 4"
        args: ["--model", "grok-4"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--reasoning-effort={value}"
      - id: grok-4-mini
        name: "Grok 4 Mini"
        args: ["--model", "grok-4-mini"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--reasoning-effort={value}"

  - id: deepseek
    name: "DeepSeek"
    category: paid
    command: ["deepseek"]
    detect:
      kind: heuristic
      prompt_regex: "^> $"
      idle_ms: 1200
    icon: "⊙"
    color: "#5E72E4"                # DeepSeek indigo; wizard + modal only
    enabled_by_default: false       # opt-in via Settings → Onboarding → models
    models:
      - id: deepseek-chat
        name: "DeepSeek Chat"
        args: ["--model", "deepseek-chat"]
      - id: deepseek-coder
        name: "DeepSeek Coder"
        args: ["--model", "deepseek-coder"]
      - id: deepseek-reasoner
        name: "DeepSeek Reasoner"
        args: ["--model", "deepseek-reasoner"]
        effort:
          options: [low, medium, high]
          default: medium
          arg_template: "--effort={value}"

  - id: kimi
    name: "Kimi (Moonshot)"
    category: paid
    command: ["kimi"]
    detect:
      kind: heuristic
      prompt_regex: "^> $"
      idle_ms: 1200
    icon: "◗"
    color: "#7C3AED"                # Moonshot purple; wizard + modal only
    enabled_by_default: false       # opt-in via Settings → Onboarding → models
    models:
      - id: kimi-k2
        name: "Kimi K2"
        args: ["--model", "kimi-k2"]
      - id: kimi-k2-mini
        name: "Kimi K2 Mini"
        args: ["--model", "kimi-k2-mini"]
```

## Tool entry schema

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Stable identifier, used in protocol and state |
| `name` | yes | Display name |
| `category` | yes | `paid` or `self_hosted`. Wizard groups by this. |
| `command` | yes | `argv[0..]` for spawn. Per-model `args` is appended. |
| `cwd_strategy` | no | `inherit` (default) or `session_dir` |
| `detect.kind` | yes | `structured` or `heuristic` |
| `detect.format` | when `kind == structured` | e.g., `claude_jsonl` |
| `detect.prompt_regex` | when `kind == heuristic` | Anchor with `^…$` for stability |
| `detect.idle_ms` | when `kind == heuristic` | Treat session as `needs_input` after this many ms idle, *if* output ends with `prompt_regex` |
| `detect.done_regex` | optional (heuristic only) | When set, the adapter emits `StateDone` whenever the (idle-gated) output window matches this pattern. Useful for tools that print a recognizable completion footer (e.g., `^✓ done`). If both `done_regex` and `prompt_regex` would match the same window, `done_regex` wins. Leave empty for tools whose prompt is identical between "awaiting" and "just finished" (codex, gemini, ollama) — use `rex complete` instead. |
| `icon` | yes | Single glyph for the wizard / modal top-strip monogram (not shown on the board) |
| `color` | yes | Tint for the monogram **only inside the wizard and the session modal**. Board rows are monochrome. |
| `enabled_by_default` | no | Bool, defaults to `true`. When `false`, the tool ships disabled — it won't appear in the wizard until the user enables it from *Settings → Onboarding → models*. Useful for emerging providers (Grok, DeepSeek, Kimi) so the default wizard stays focused. |
| `models` | yes | At least one entry |

## Model entry schema

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Stable identifier |
| `name` | yes | Display name |
| `args` | yes (unless `args_prompt` is set) | Flags appended after `command` |
| `args_prompt` | optional | Prompts the user for a single free-form value at launch; the value is appended to `command` (used by Ollama "custom tag") |
| `effort.options` | optional | Wizard step 3 is skipped when this block is absent |
| `effort.default` | optional | Must be a member of `options` |
| `effort.arg_template` | optional | String with `{value}` placeholder; interpolated and appended to args |

## Adding a new model

Drop this into `~/.config/rex/tools.yaml`:

```yaml
tools:
  - id: claude
    models:
      - id: opus-with-vision
        name: "Opus 4.7 (vision)"
        args: ["--model", "opus", "--vision"]
        effort:
          options: [default, high]
          default: high
          arg_template: "--effort={value}"
```

Next time you press `n`, the new model appears under Claude Code at step 2 of the wizard. The daemon re-reads `tools.yaml` on `SIGHUP` (and on next start); no hot-reload via filesystem watch in v0.

## Adding a new tool

```yaml
tools:
  - id: aider
    name: "aider"
    category: paid
    command: ["aider", "--no-stream"]
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
```

## Adding a new detection scheme

This is the only path that requires a Go change. Implement `Adapter` with a unique `kind`, register it in `internal/registry/adapters.go`, and reference the new `kind` from YAML.
