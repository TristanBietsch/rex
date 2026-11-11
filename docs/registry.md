# Tool registry

The registry defines agent CLIs Rex can spawn. Built-in tools ship embedded in the binary. User tools merge from `~/.config/rex/tools.yaml`.

## NAME

tools.yaml — tool and model catalog

## SYNOPSIS

```yaml
tools:
  - id: <string>
    name: <string>
    category: <string>
    command: [<argv>...]
    cwd_strategy: inherit | session_dir   # optional
    detect: { ... }
    icon: <string>
    color: <string>
    enabled_by_default: <bool>            # optional; builtin only
    trust_prompt: { ... }                 # optional
    models:
      - id: <string>
        name: <string>
        args: [<argv>...]
        args_prompt: <string>             # optional
        effort: { ... }                   # optional
```

## DESCRIPTION

Each tool is one agent executable. Each model is one variant (arguments and optional effort tier). At spawn time Rex resolves `tool_id`, `model_id`, and optional `effort`, then execs `command` plus model `args` and effort substitution.

State detection (`detect`) selects an adapter that classifies PTY output into session states.

## Merge rules

1. Embedded `builtin.yaml` always loads.
2. User file at `-tools` / `~/.config/rex/tools.yaml` merges when present. Missing file is OK.
3. Merge is by tool `id`. User fields replace builtin scalars when non-empty.
4. Models merge by model `id` (replace or append).
5. `enabled_by_default` is **not** merged from user YAML; it applies only on builtin entries.

Duplicate tool or model ids are rejected at load time.

## Tool fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Stable lowercase id (`claude`, `echo`) |
| `name` | yes | Display name |
| `category` | yes | Label only (`paid`, `self_hosted`) |
| `command` | yes | Base argv before model args |
| `cwd_strategy` | no | `inherit` (default) or `session_dir` |
| `detect` | yes | State classifier config |
| `icon`, `color` | yes | TUI display |
| `enabled_by_default` | no | If false, hidden from wizard until enabled |
| `models` | yes | At least one model |

## detect

| `kind` | Required fields | Behavior |
|--------|-----------------|----------|
| `heuristic` | `prompt_regex`, `idle_ms` (>0) | Regex on sanitized output; optional `done_regex` |
| `hooks` | `format` | Agent hooks report state; only `claude` is implemented |

`hooks` / `claude`: at spawn the daemon adds `--settings <json>` with Claude Code hooks and sets `REX_HOOK_FILE` in the child env. Each hook appends a state word to `sessions/<id>/hooks.log`:

| Hook | State |
|------|-------|
| `SessionStart`, `Notification`, `Stop` | `needs_input` |
| `UserPromptSubmit`, `PreToolUse`, `PostToolUse` | `working` |

Two more hooks track subagents. `SubagentStart` appends `agent+ <agent_id>` and `SubagentStop` appends `agent- <agent_id>`, reading both fields from the hook's JSON. Events without an `agent_type` are Claude's internal helpers and are skipped. `SessionStart` first writes `agents-reset`. The adapter counts a set of ids (a resumed subagent starts again with the same id), and the board shows it as the subagent column. The count drops to 0 when the agent exits.

A `working` state with no visible PTY output for 15s becomes `needs_input` (an Esc-interrupted turn fires no `Stop`). Heuristic tools apply the same rule when `prompt_regex` doesn't match: a screen frozen for 15s (auth, trust or menu dialog) is waiting on you; working agents always stream output or animate a spinner.

State semantics for all kinds: a finished agent turn is `needs_input` (waiting on you). `done` comes from process exit (code 0), the `Complete` intent, or a heuristic `done_regex` match; once `done`, later output can't move the session back.

### Initial prompt delivery

The wizard / quick-spawn task reaches the agent on argv where the CLI supports it: `claude … -- <prompt>`, `codex … -- <prompt>`, `gemini … --prompt-interactive=<prompt>`. Other tools (ollama, custom) get it pasted into the PTY once `prompt_regex` matches.

### trust_prompt

Describes the agent's folder-trust dialog. The daemon answers it only for sessions under a folder in the `auto_trust_dirs` setting ([settings.md](settings.md#auto-trust)).

| Field | Description |
|-------|-------------|
| `shown` | Regex on the screen text: the dialog is up |
| `selected` | Regex: the accept option is the selected one |
| `move` | Keys that move the selection one step (Claude: Down, `"\e[B"`) |
| `confirm` | Keys that confirm (Claude: Enter, `"\r"`) |

While `shown` matches, the daemon sends `move` (at most 4 times, 300 ms apart) until `selected` matches, then sends `confirm` once. All four fields are required, and both regexes must compile. Anchor `shown` on the dialog's own header, so text that only quotes an option never triggers it, and keep `selected` on one line (`[ \t]`, not `\s`). The built-in `claude` entry needs "Accessing workspace:" (or the older "Do you trust the files in this folder?") followed by "Yes, I trust this folder" (or "Yes, proceed").

## Model fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Model id within tool |
| `name` | yes | Display name |
| `args` | no | Extra argv appended after `command` |
| `args_prompt` | no | Free-form value collected at spawn |
| `effort` | no | Reasoning-effort axis |

## effort

```yaml
effort:
  options: [low, medium, high]
  default: medium
  arg_template: "--effort={value}"
```

`{value}` is replaced with the chosen effort string.

## Built-in tools

Shipped ids include: `echo` (test), `claude`, `codex`, `gemini`, `ollama`, `grok`, `deepseek`, `kimi`. The last three default to `enabled_by_default: false`.

## Reload

Edit `tools.yaml`, then:

```sh
rex reload
```

SIGHUP reloads the registry in a running daemon. Running sessions keep their original command and adapter.

## EXAMPLES

Minimal overlay:

```yaml
tools:
  - id: my-agent
    name: My Agent
    category: self_hosted
    command: [my-agent-cli]
    detect:
      kind: heuristic
      prompt_regex: "(?m)^> "
      idle_ms: 1000
    icon: "●"
    color: "#888888"
    models:
      - id: default
        name: Default
        args: []
```

See [testdata/tools-user.yaml](../testdata/tools-user.yaml) in the repository.

## VALIDATION ERRORS

Load fails when: no tools; duplicate ids; tool without models; invalid `detect` kind; heuristic without `prompt_regex` or `idle_ms`; structured without supported `format`.

## SEE ALSO

- [howto/add-a-tool.md](howto/add-a-tool.md)
- [daemon.md](daemon.md) — `-tools` flag
- [settings.md](settings.md) — spawn defaults
- [protocol.md](protocol.md) — `NewSession` intent
