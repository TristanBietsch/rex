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
    trust_args: [<argv>...]               # optional
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
6. `trust_prompt` and `trust_args` replace the builtin value when set; an empty value cannot clear it. To stop auto-trust, empty `auto_trust_dirs` instead.
7. A partial overlay of a retired built-in tool (`codex`, `gemini`, `ollama`, `grok`, `deepseek`, `kimi`) that has no built-in entry and is not a complete tool definition is dropped with a warning instead of failing the load. The daemon logs the warning at start and on reload, and the TUI splash shows it. Define the tool fully in `tools.yaml` to keep it. Any other invalid tool still fails the load.

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
| `heuristic` | `prompt_regex`, `idle_ms` (>0) | Regex on sanitized output; optional `done_regex` and `busy_regex` |
| `hooks` | `format` | Agent hooks report state; only `claude` is implemented |

`busy_regex` (heuristic, optional) is text the agent shows only while it works, such as `agy`'s `esc to cancel` footer. Detection reads raw output, where an old idle footer can linger after the agent swaps it, so busy wins when its last match comes after the last `prompt_regex` match. A quiet stretch mid-turn then stays `working`. A screen frozen for 15 s still reads as waiting, which covers a permission question.

`hooks` / `claude`: at spawn the daemon adds `--settings <json>` with Claude Code hooks and sets `REX_HOOK_FILE` in the child env. Each hook appends a state word to `sessions/<id>/hooks.log`:

| Hook | State |
|------|-------|
| `SessionStart`, `Notification`, `Stop` | `needs_input` |
| `UserPromptSubmit`, `PreToolUse`, `PostToolUse` | `working` |

Subagents are tracked in the same file:

- `SubagentStart` appends `agent+ <agent_id>` and `SubagentStop` appends `agent- <agent_id>`. The id comes from the hook JSON's top-level `agent_id`; nested objects, arrays and pretty-printing are handled. Events without an `agent_type` are Claude's internal helpers and are skipped.
- `PostToolUse` of a subagent's tool call (its JSON has an `agent_id`) also appends `agent~ <agent_id>` as a sign of life.
- `SessionStart` writes `agents-reset` first for a new or cleared conversation, but not after a compaction (`source: compact`), when subagents may still be running.

The adapter counts a set of ids: a resumed subagent starts again with the same id. An id with no start or tool call for 15 minutes stops counting, which bounds a count stuck by a lost `SubagentStop`. Its next tool call counts it again. The board shows the count as the subagent column, and it drops to 0 when the agent exits.

A `working` state with no visible PTY output for 15s becomes `needs_input` (an Esc-interrupted turn fires no `Stop`). Heuristic tools apply the same rule when `prompt_regex` doesn't match: a screen frozen for 15s (auth, trust or menu dialog) is waiting on you; working agents always stream output or animate a spinner.

State semantics for all kinds: a finished agent turn is `needs_input` (waiting on you). `done` comes from process exit (code 0), the `Complete` intent, or a heuristic `done_regex` match; once `done`, later output can't move the session back.

### Initial prompt delivery

The wizard / quick-spawn task reaches the agent on argv where the CLI supports it: `claude … -- <prompt>`, `codex … -- <prompt>`, `agy … --prompt-interactive=<prompt>` (antigravity; also a user-defined `gemini` tool). Other tools (ollama, custom) get it pasted into the PTY once `prompt_regex` matches.

### trust_prompt

Describes the agent's folder-trust dialog. The daemon answers it only for sessions under a folder in the `auto_trust_dirs` setting ([settings.md](settings.md#auto-trust)).

| Field | Description |
|-------|-------------|
| `shown` | Regex on the screen text: the dialog is up |
| `selected` | Regex: the accept option is the selected one |
| `move` | Keys that move the selection one step (Claude: Down, `"\e[B"`) |
| `confirm` | Keys that confirm (Claude: Enter, `"\r"`) |

While `shown` matches, the daemon sends `move` (at most 4 times, 300 ms apart) until `selected` matches, then sends `confirm` once. All four fields are required, and both regexes must compile. Anchor `shown` on the dialog's own header, so text that only quotes an option never triggers it, and keep `selected` on one line (`[ \t]`, not `\s`). The built-in `claude` entry needs "Accessing workspace:" (or the older "Do you trust the files in this folder?") followed by "Yes, I trust this folder" (or "Yes, proceed").

### trust_args

Extra arguments that make the agent trust the session folder itself, so no dialog shows. They are added only for sessions under a folder in `auto_trust_dirs`, and one argument must contain `{cwd_list}`. That placeholder expands to a JSON array holding the folder as given and, when different, as resolved through symlinks. Prefer this to `trust_prompt` when the agent supports it: nothing is typed into the agent.

When a tool sets both, both apply: the arguments make the dialog unnecessary, and the answerer stays armed as a fallback. If your `command` or model `args` already pass the same flag (for `agy`, your own `--settings`), the agent gets it twice and which one wins is up to the agent.

The built-in `antigravity` entry uses `agy`'s session-only settings, which are never saved to its `settings.json`:

```yaml
trust_args: ["--settings", "{\"trustedWorkspaces\":{cwd_list}}"]
```

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

Shipped ids include: `echo` (test), `claude`, `codex`, `antigravity`, `ollama`, `grok`, `deepseek`, `kimi`. The last three default to `enabled_by_default: false`.

`antigravity` runs the Antigravity CLI (`agy`), Google's successor to Gemini CLI, which was retired on 2026-06-18. `gemini` stays built in but hidden (`enabled_by_default: false`), so an existing install, a partial `gemini` overlay in `tools.yaml`, or a saved `default_spawn_tool: gemini` keeps working. The Gemini model's effort picks its slug (`--model=gemini-3.8-flash-<effort>`); the other models take `--effort`. Model slugs come from `agy models` and depend on your account.

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
