# R01-3 Prompt templates

## Summary
- Reusable prompts as markdown files. Global: `~/.config/rex/prompts/<name>.md`; per workspace: `.rex/prompts/<name>.md` (R06, only in trusted workspaces). Same idea as opencode's custom commands.
- Use: `:p <name> [args]` on the board spawns a session from the template; the wizard gets a "from template" first step; `rex new -template <name> [args]`.
- Behind `prompt_templates` (on).

## Current behavior
- Every prompt is typed by hand (`i` quick spawn, `n` wizard, `rex new "<text>"`).
- P1 (round 1) adds name, model and skills to the wizard and `rex new -name/-skill`.

## Design

### File format
```markdown
---
description: Fix the first failing test and stop
tool: claude          # optional; default_spawn_tool when absent
model: sonnet         # optional
effort: medium        # optional
skills: [go-testing]  # optional, P1 skill library names
name: "fix {{1}}"     # optional display name, may use args
args:                 # optional, documents positional args for :p completion
  - name: package
    default: ./...
---
Run `go test {{1}}` and fix the first failure. Keep the change minimal.
{{rest}}
```
- Placeholders (D1): `{{1}}`…`{{9}}` positional args, `{{rest}}` everything after the last used positional, `{{cwd}}`, `{{branch}}` (git branch of cwd, empty if none). Unknown placeholders are an error at load time, not at spawn.
- Name = file name without `.md`, `[a-z0-9-]{1,40}`. Workspace template wins over a global one with the same name; the picker shows its source.
- Files are data: no shell expansion, no command execution inside templates (D2).

### UX
- `:p <name> [args]`: spawns immediately with the template's tool/model/effort/skills; args are shell-like split (quotes respected).
- `:p` alone or `:p <prefix>`: picker overlay with name, description and source; `enter` fills the prompt line so the user can edit before spawn.
- Wizard: first step "Blank / from template"; choosing a template prefills every later step (editable).
- `rex templates ls|show <name>|edit <name>|new <name>`; `rex new -template <name> -- args…`.
- Live reload: the prompts folder is watched with U2's poller (same `live_reload` flag).

## Feature flag
- `prompt_templates` (on). Off = folders not read, `:p` / `rex templates` / `-template` answer "off", wizard has no template step.
- Off-is-inert test in R01-3-1.

## Plan
1. **R01-3-1 Loader + renderer.** `internal/features/templates`: `Load(dirs []Source) ([]Template, []error)`, `Render(t Template, args []string, ctx RenderCtx) (Spawn, error)`; `Spawn{Prompt, Tool, Model, Effort, Name string; Skills []string}`. Tests: frontmatter parsing; placeholder table incl. `{{rest}}`; unknown placeholder rejected at load; workspace overrides global; bad name ignored with a warning; model not offered by the tool → load warning, spawn falls back to the default with a notice.
2. **R01-3-2 `:p` command + picker.** Command-table row (U1-7), picker overlay, prompt-line prefill. Goldens; arg splitting tests (quotes, escaped quotes).
3. **R01-3-3 Wizard step.** "Blank / from template" step before provider; prefill. Tests: every later step shows template values and stays editable.
4. **R01-3-4 CLI.** `rex templates …`, `rex new -template`. `new <name>` writes a commented starter file (0600) and opens `$EDITOR`.
5. **R01-3-5 Docs + CHANGELOG.** `docs/templates.md` (new) with three examples.

## Review focus
- Template from a cloned repo's `.rex/prompts/` contains prompt-injection text: only loaded in R06-trusted workspaces; picker marks workspace templates.
- `{{branch}}` in a folder that is not a repo: empty string, never an error.
- Arg containing `{{1}}` literally: substituted once, never re-expanded.
- Very long template (> 64 KiB): rejected at load.
- Windows line endings in template files: normalized.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Placeholder syntax | `{{1}}`…`{{9}}`, `{{rest}}`, `{{cwd}}`, `{{branch}}`. |
| D2 | Dynamic content | None. Templates are plain data; nothing runs. |
| D3 | `:p <name> args` | Spawns immediately; `:p` alone opens the picker, which fills the prompt line for editing. |
