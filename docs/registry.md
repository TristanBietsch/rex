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
    skip_permissions_args: [<argv>...]    # optional
    skill_args: [<argv>...]               # optional; must hold {plugin_dir}
    resume: { ... }                       # optional
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
6. `trust_prompt`, `trust_args`, `skip_permissions_args`, `skill_args`, `resume`, `status_env`, `models_cmd`, `model_arg_template`, `model_effort`, `prompt_args`, `prompt_paste`, `login_regex` and `install_hint` replace the builtin value when set (`prompt_args` and `prompt_paste` replace each other); an empty value cannot clear it. To stop auto-trust, empty `auto_trust_dirs` instead. A `resume` overlay replaces the whole block (no field-by-field merge).
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
| `skip_permissions_args` | no | Extra argv added only while the `skip_permissions` setting is on (the built-in `claude` and `antigravity` use `["--dangerously-skip-permissions"]`). An overlay replaces the built-in value; no argument may be empty |
| `skill_args` | no | Extra argv that load the skills rex stages for one session. Must contain `{plugin_dir}` (the session's plugin folder, one argument); no argument may be empty. The built-in `claude` uses `["--plugin-dir", "{plugin_dir}"]`; `antigravity` has none, so it cannot load skills per session. See [skills](howto/skills.md) |
| `status_env` | no | `NAME=value` entries added to the child's environment to turn its status channel on (`antigravity`: `["AGY_CLI_OSC7501=on"]`). Needs `detect.status`; names must be plain identifiers. Tools that answer the OSC 7501 query (`qwen`, `grok`) need none |
| `prompt_args` | no | How the task text reaches the agent on argv, appended after every other argument. Must hold `{prompt}` exactly once; the text stays one argument whatever it holds (spaces, quotes). When `{prompt}` is an element of its own with no `--` before it, a task that starts with `-` gets one space in front so it can never read as an option; after `--`, or inside an element such as `--prompt-interactive={prompt}`, it is passed as is. See [Initial prompt delivery](#initial-prompt-delivery) |
| `prompt_paste` | no | `{plain: bool, settle_ms: N}`: paste the task into the PTY once output has been quiet for `settle_ms` (0-10000), for REPLs where argv would run one-shot (`ollama`). Exclusive with `prompt_args` |
| `install_hint` | no | Text shown in Settings → Onboarding next to the tool while its binary is not on `PATH`: a command or URL for the user to read (rex never runs it). Empty: `install <command> and put it on your PATH`. A user `tools.yaml` replaces the built-in hint |
| `login_regex` | no | Regex on the screen text that shows the agent is signed out. While it is the latest thing shown (no `prompt_regex` match after it) the session is `needs_input` and its last line reads `<id>: not signed in — run `<command> login``. Must compile |
| `models_cmd` | no | How rex asks the agent for its own model list. See [models_cmd](#models_cmd) |
| `model_arg_template` | with `models_cmd` | Argv for a detected model, `{model}` replaced by its id: `["--model", "{model}"]`. One element each, never split. Required when `models_cmd` is set |
| `model_effort` | no | An `effort` block that detected models get (a detected model has no hand-written one). `qwen` and `grok` set it; `cursor` encodes effort in the model id and has none |
| `detect` | yes | State classifier config |
| `icon`, `color` | yes | TUI display (wizard, pickers). `icon` is one character one cell wide; each built-in has its own (`claude` `◆`, `codex` `◇`, `antigravity` `◈`, `ollama` `◉`, `qwen` `▣`, `cursor` `▲`, `grok` `◢`) |
| `enabled_by_default` | no | If false, hidden from wizard until enabled |
| `models` | yes | At least one model |

## detect

| `kind` | Required fields | Behavior |
|--------|-----------------|----------|
| `heuristic` | `prompt_regex`, `idle_ms` (>0) | Regex on sanitized output; optional `done_regex` and `busy_regex` |
| `hooks` | `format` | Agent hooks report state; only `claude` is implemented |

`busy_regex` (heuristic, optional) is text the agent shows only while it works, such as `agy`'s `esc to cancel` footer. Detection reads raw output, where an old idle footer can linger after the agent swaps it, so busy wins when its last match comes after the last `prompt_regex` match. A quiet stretch mid-turn then stays `working`. A screen frozen for 15 s still reads as waiting, which covers a permission question.

`status` (heuristic, optional) names a per-session status channel the agent reports its state on. The only value is `osc7501`, the Program Status Protocol used by Antigravity, Qwen Code and Grok Build. For such a tool the daemon:

- **answers the support query.** When the output holds `ESC ] 7501 ; ? BEL` (or ST), even split across reads, it writes `ESC ] 7501 ; ? : states=idle,working,done,blocked,error ESC \` to the child's PTY, once per query. That is all `qwen` and `grok` need to start reporting; no tool gets a reply unless it has `status: osc7501`;
- **removes `TMUX`, `TMUX_PANE`, `TERM_PROGRAM` and `TERM_PROGRAM_VERSION`** from the child's environment (the agent runs in rex's PTY, not tmux, and grok never asks for the channel when it thinks it is inside tmux);
- adds the tool's `status_env` after your own environment, so it wins over an `off` (`antigravity` needs `AGY_CLI_OSC7501=on`);
- reads `ESC ] 7501 ; state=<word> : ... BEL` from the output (ST also ends it). `working` is `working`; `idle` (turn finished), `done`, `blocked` (a question, a permission, the trust dialog, an auth screen) and `error` are `needs_input`, and `clear` changes nothing. The session's `wait_reason` is `review` after `done` (kept through the `idle` that follows) and `input` after `blocked` or `error`; the report's `kind` (`permission`, `question`, `auth`) is kept in the adapter, not sent on the wire. Process exit still decides `done` and `failed`. Unknown states and keys are ignored, and a sequence longer than 512 bytes is dropped. Before the first sequence the footer heuristic decides, and a `working` report followed by 15 s of silence reads as waiting (Esc sends no idle sequence). The optional `msg` key is base64 text from the agent; it is decoded, sanitized and kept only for a future preview, never logged. Nothing is written to `~/.gemini`: antigravity hooks are not used. `status` needs `kind: heuristic`.

`hooks` / `claude`: at spawn the daemon adds `--settings <json>` with Claude Code hooks and sets `REX_HOOK_FILE` in the child env. Each hook appends a state word to `sessions/<id>/hooks.log`:

| Hook | State |
|------|-------|
| `SessionStart` (not after a compaction), `Notification`, `Stop`, `StopFailure` | `needs_input` |
| `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure` | `working` |

Tool waits are tracked in the same file (they feed the board's Waiting section):

- A main-thread `PreToolUse` (no `agent_id` in the hook JSON) of `Bash`, `Task` / `Agent` or `WebFetch` / `WebSearch` appends `wait+ shell`, `wait+ subagent` or `wait+ web` after the state word, in the same write. Any other main-thread tool appends `wait-`; a subagent's tool call appends neither.
- A main-thread `PostToolUse` or `PostToolUseFailure` appends `wait-`; for an `Agent` / `Task` call that returned at once (`async_launched`, a background subagent) `wait- launch`. `UserPromptSubmit` appends `prompt`, which ends the last turn's wait. `Stop`, `StopFailure` and `SessionStart` end a wait through their `needs_input` word.
- `Notification` appends `notify <notification_type>`; the type decides, not the word. `permission_prompt`, `elicitation_dialog`, `elicitation_url_dialog`, `agent_needs_input`, `quota_auto_resume_stale` and a missing type are questions (READY at once; an open wait goes on after the answer). `idle_prompt` and `quota_auto_resume_disabled` end the turn. Every other type, including ones newer than rex, changes nothing.
- A `wait-` while other subagents still run leaves a `subagent` wait on until the last one stops (several `Task` calls in parallel).

Subagents are tracked in the same file:

- `SubagentStart` appends `agent+ <agent_id>` and `SubagentStop` appends `agent- <agent_id>`. The id comes from the hook JSON's top-level `agent_id`; nested objects, arrays and pretty-printing are handled. Events without an `agent_type` are Claude's internal helpers and are skipped.
- `PostToolUse` of a subagent's tool call (its JSON has an `agent_id`) also appends `agent~ <agent_id>` as a sign of life. A sign of life for a subagent that already stopped is ignored.
- `PreToolUse` of a subagent's own `Agent` / `Task` call appends `nest+`: the next subagent started is nested and reports to its parent.
- `Stop` and `StopFailure` append `bg-agents <n>` when the hook input has `background_tasks`: `n` counts its `"type": "subagent"` entries. Running subagents above `n` (oldest sign of life first) are dropped: their `SubagentStop` was lost.
- A background subagent that ends owes the main agent a report, which Claude delivers as a `UserPromptSubmit`; each `prompt` line pays one.
- `SessionStart` writes `agents-reset` first for a new or cleared conversation. After a compaction (`source: compact`) it writes `compacted` instead of a state word: subagents may still be running and an auto-compaction happens mid-turn.
- `SessionStart` (every source, compaction included) ends with `sid <session_id>`, the conversation's resume id. The adapter keeps the latest valid one (a canonical UUID; anything else is ignored); it is not a state change.

The adapter counts a set of ids: a resumed subagent starts again with the same id. An id with no start or tool call for 15 minutes stops counting, which bounds a count stuck by a lost `SubagentStop`. Its next tool call counts it again. The board shows the count as the subagent column, and it drops to 0 when the agent exits.

A Claude turn with no hook and no visible PTY output for 30 s (15 min with a tool wait open, never while a subagent runs) becomes `needs_input` (an Esc-interrupted turn fires no `Stop`; the Esc key itself ends it sooner, see [tui.md](tui.md)). Heuristic tools apply the same rule when `prompt_regex` doesn't match: a screen frozen for 15s (auth, trust or menu dialog) is waiting on you; working agents always stream output or animate a spinner.

State semantics for all kinds: a finished agent turn is `needs_input` (waiting on you). `done` comes from process exit (code 0), the `Complete` intent, or a heuristic `done_regex` match; once `done`, later output can't move the session back.

### Initial prompt delivery

The wizard / quick-spawn task reaches the agent through the tool's `prompt_args` (argv, after every other argument), or `prompt_paste` (pasted into the PTY), or, with neither, pasted once `prompt_regex` matches. Built in:

| Tool | Delivery |
|------|----------|
| `claude`, `codex` | `prompt_args: ["--", "{prompt}"]` |
| `antigravity`, `gemini` | `prompt_args: ["--prompt-interactive={prompt}"]` (a bare positional or `-p` is one-shot) |
| `qwen` | `prompt_args: ["--prompt-interactive={prompt}"]` (the `=` form, like agy) |
| `grok`, `cursor` | `prompt_args: ["{prompt}"]` (a positional prompt starts an interactive session). The daemon puts one space in front of a task that starts with `-`, so it never parses as an option |
| `ollama` | `prompt_paste: {plain: true, settle_ms: 800}` (`ollama run MODEL PROMPT` answers once and exits) |

### models_cmd

```yaml
models_cmd:
  args: ["--list-models"]      # appended to command (required, no empty argument)
  parse: lines                 # lines (default) | json
  id_regex: "\\(([^)]+)\\)\\s*$"   # lines: capture group 1 is the id; a line that does not match is skipped
  skip_header: 1               # lines: leading lines to drop (0-20)
  # json_path: "models[].id"   # json: dot-separated keys, "key[]" walks an array
  timeout_ms: 5000             # 100-30000, default 5000
model_arg_template: ["--model", "{model}"]
```

The daemon runs `command` plus `args` with no stdin, its own environment, the timeout, and at most 64 KiB of stdout (more counts as broken, not truncated; stderr is dropped). The command is fixed here, never built from session output. Each id must match `^[A-Za-z0-9._:/@\[\]-]{1,128}$` (and not start with `-`), or it is dropped. A model becomes `{id, name: id, args: model_arg_template with {model} = id, effort: model_effort}`.

Detected models come first in the list, then the tool's static `models` it did not list; a detected id that is also static keeps the static name, args and effort. A timeout, a non-zero exit, oversized output, or output with no valid id (an auth error, a usage text) logs one line and leaves the static list. The result is cached in `~/.local/state/rex/models-cache.json` ([paths.md](paths.md)), keyed by tool id, binary path and mtime and the command, and trusted for 24 hours; the daemon refreshes it at start and on `rex reload` (which ignores a fresh entry). Only installed tools are asked. The `model_autodetect` feature switch turns all of this off ([settings.md](settings.md#features)).

Built-in lists: `qwen --list-models` (`Name (id)`, header `MODEL`), `grok models` (`  * id (default)`), `cursor-agent models` (`id - Name`, two header lines).

### Installed tools

A tool is installed when the first word of `command` is found on the daemon's `PATH` (`exec.LookPath`, which applies `PATHEXT` on Windows). The daemon works this out at start and on every reload and sends it as `installed` on each tool of the `Snapshot` ([protocol.md](protocol.md)). The new-session wizard and the quick-spawn pickers offer installed tools only (the TUI reads `Snapshot.tools` and `ToolsChanged`); Settings → Onboarding lists the missing ones dimmed with their `install_hint`.

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

Extra arguments that make the agent trust the session folder itself, so no dialog shows. They are added only for sessions under a folder in `auto_trust_dirs`. An argument may contain `{cwd_list}`, which expands to a JSON array holding the folder as given and, when different, as resolved through symlinks; a switch that turns the check off for the whole run needs no placeholder when the tool sets `trust_args_global: true` (without it a `trust_args` that lacks `{cwd_list}` is rejected, which catches a typo). `qwen`: `["--settings", "{\"security\":{\"folderTrust\":{\"enabled\":false}}}"]`, session-only, nothing saved). Prefer this to `trust_prompt` when the agent supports it: nothing is typed into the agent.

When a tool sets both, both apply: the arguments make the dialog unnecessary, and the answerer stays armed as a fallback. If your `command` or model `args` already pass the same flag (for `agy`, your own `--settings`), the agent gets it twice and which one wins is up to the agent.

The built-in `antigravity` entry uses `agy`'s session-only settings, which are never saved to its `settings.json`:

```yaml
trust_args: ["--settings", "{\"trustedWorkspaces\":{cwd_list}}"]
```

It also has a `trust_prompt` fallback for a future `agy` that ignores `--settings`: it answers "Do you trust the contents of this project?" (`> Yes, I trust this folder` selected first) in trusted folders only.

### resume

How an agent's conversation is identified and resumed. Without it the tool cannot be resumed.

```yaml
resume:
  args: ["--resume", "{id}"]            # required: argv that resumes a conversation
  new_args: ["--session-id", "{id}"]    # optional: added at spawn with a fresh UUID
  capture_regex: "id=([0-9a-f-]{36})"   # optional: group 1 = id, read at child exit
```

`{id}` is the placeholder for the conversation id. It may sit inside one argument (`--conversation={id}`); substitution never splits or adds arguments. This differs from effort's `{value}`, which is split on whitespace. `args` is required and must contain `{id}`; `new_args`, when set, must contain `{id}`; `capture_regex` must compile and have at least one group (it runs in multi-line mode).

Rex learns the id from up to three sources; a later one overwrites an earlier one:

1. **Spawn.** With `new_args`, rex picks a fresh random UUID (not the rex session id) and adds the substituted `new_args` after the trust and skip-permissions arguments, before the hook arguments and the prompt. The built-in `claude` uses this (`--session-id`). The id is in the session's first `SessionAdded`.
2. **The agent's own report.** Claude Code's session-scoped SessionStart hook reports its `session_id` (a `sid` line in the hook file), so the id stays right after `/clear` or an in-app `/resume`.
3. **`capture_regex`**, run when the child exits on the normalized screen text (escape sequences stripped). The last match's group 1 is used.

Every id must be a canonical UUID (either case; stored lowercase), because it becomes a command-line argument. Anything else is ignored. The built-in `antigravity` resumes with `--conversation={id}`. It has no `new_args` (agy cannot preassign an id), so the id comes from `capture_regex` only: on exit agy prints `Resume with -c (or command below):` and then `agy --conversation=<uuid>`, which `agy --conversation=([0-9a-fA-F-]{36})` reads.

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

Shipped ids include: `echo` (test), `claude`, `codex`, `antigravity`, `qwen`, `cursor`, `grok`, `ollama`, `deepseek`, `kimi`. `deepseek` and `kimi` default to `enabled_by_default: false`.

`qwen` (Qwen Code), `cursor` (Cursor CLI, binary `cursor-agent`) and `grok` (Grok Build) are `enabled_by_default: true`: they are visible when installed. The wizard offers each only while its binary is on the daemon's `PATH` (the `installed` flag in [protocol.md](protocol.md)) and the `extra_agents` feature is on; `rex setup` applies the same rule with its own `PATH`. `qwen` and `grok` report their state on OSC 7501; `cursor` has no status channel, so it uses screen patterns only.

**Permissions and trust of the new agents.** Check the agent's own safety settings before you point it at a folder you care about:

- `grok`'s default mode can edit files and run commands **without asking**. rex does not change its mode (that is your grok config); `skip_permissions` adds `--always-approve` on top.
- `cursor`'s default mode runs safe tool calls, including file edits, without asking; shell commands ask. `skip_permissions` adds `--yolo`.
- `qwen` asks before it applies a change; `skip_permissions` adds `--dangerously-skip-permissions`.
- rex never answers the folder-trust dialog of `grok` or `cursor` and never passes `--trust` (it is saved globally by those agents). A new folder shows "needs you" until you press the key once. `auto_trust_dirs` does not apply to them. `qwen` has a session-only switch (`trust_args`), so it is the only one of the three that `auto_trust_dirs` covers.

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

Load fails when: no tools; duplicate ids; tool without models; invalid `detect` kind; heuristic without `prompt_regex` or `idle_ms`; structured without supported `format`; `status_env` without `detect.status` or an entry that is not `NAME=value`; `models_cmd` with no `args`, a `parse` other than `lines` or `json`, `json` without a valid `json_path`, an `id_regex` that does not compile or has no capture group, `skip_header` outside 0-20, `timeout_ms` outside 100-30000, or no `model_arg_template`; `model_arg_template` without `{model}`; `prompt_args` without exactly one `{prompt}` (or together with `prompt_paste`); `prompt_paste.settle_ms` outside 0-10000; a `login_regex` that does not compile.

## SEE ALSO

- [howto/add-a-tool.md](howto/add-a-tool.md)
- [daemon.md](daemon.md) — `-tools` flag
- [settings.md](settings.md) — spawn defaults
- [protocol.md](protocol.md) — `NewSession` intent
