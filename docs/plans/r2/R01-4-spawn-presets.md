# R01-4 Spawn presets

## Summary
- A preset is a saved spawn configuration: tool, model, effort, skills, an optional name prefix and an optional prompt prefix. Like opencode's agents (build / plan), but for rex's spawn wizard.
- The wizard shows presets as its first step; `i` quick spawn can use a preset; `rex new -preset <name>`; R13 auto model can map a class to a preset instead of a bare model.
- Behind `spawn_presets` (on).

## Current behavior
- Quick spawn uses `default_spawn_tool/model/effort` (`update.go:402`, `quickSpawnSlug` `:580`).
- The wizard walks provider → model → effort → (P1) name → skills → describe every time.
- R01-3 templates carry a prompt; presets carry no prompt body, only how to run.

## Design

### Storage (D1)
- `~/.config/rex/presets.yaml` (one file, list) and `.rex/presets.yaml` (R06 trusted workspaces). Example:
  ```yaml
  presets:
    - id: plan
      label: "Plan only"
      tool: claude
      model: opus
      effort: high
      skills: [planning]
      name_prefix: "plan-"
      prompt_prefix: "Do not edit files. Produce a plan only.\n\n"
    - id: quick
      label: "Quick fix"
      tool: claude
      model: haiku
      effort: low
  ```
- Validation at load: tool exists and is installed (R02), model offered by the tool, effort valid for the model, skills exist (P1 library). Invalid presets are listed with the reason and not shown in the wizard.
- `quick_spawn_preset` setting (empty = use the three `default_spawn_*` settings as today). When set, `i` uses the preset.

### UX
- Wizard first step: presets (with label and a one-line summary `claude · opus · high · 1 skill`), then "Custom…" which continues the normal steps. Choosing a preset jumps to the describe step; `shift+tab` goes back to edit any field.
- Save from the wizard: on the summary step, `s` saves the current choices as a new preset (asks for id and label).
- `:preset <id>` sets the quick-spawn preset for this TUI run only; Settings sets it persistently.
- `rex presets ls|show|rm`, `rex new -preset <id>`.
- R13: `route_map` values may be `preset:<id>` as well as `model/effort`.
- R01-3: a template may name `preset: <id>` instead of tool/model/effort/skills.

## Feature flag
- `spawn_presets` (on). Off = presets file not read, wizard has no preset step, `quick_spawn_preset` ignored, `-preset` and `rex presets` answer "off".
- Off-is-inert test in R01-4-1.

## Plan
1. **R01-4-1 Loader + validation.** `internal/catalog/presets`: `Load(paths) ([]Preset, []Invalid)`, `Apply(p Preset, base SpawnSpecInput) SpawnSpecInput`. Tests: validation table; workspace preset overrides global by id; prompt_prefix applied once; uninstalled tool → invalid with reason.
2. **R01-4-2 Wizard preset step + save.** Goldens; `shift+tab` back-edit; save writes YAML with sparse read-modify-write (same rule as U2-2).
3. **R01-4-3 Quick spawn + setting.** `quick_spawn_preset` with dynamic options; `:preset`. Tests: quick spawn argv equals the preset's.
4. **R01-4-4 CLI + R13/R01-3 hooks.** `rex presets`, `-preset`; `route_map` `preset:` values; template `preset:` key. Tests for each consumer.
5. **R01-4-5 Docs + CHANGELOG.**

## Review focus
- Preset references a skill that was deleted later: preset marked invalid, existing sessions unaffected.
- Preset with `skip_permissions`-like args: presets cannot carry raw argv; only the typed fields above (prevents bypassing K1's opt-in).
- `prompt_prefix` plus a template body plus user text: order fixed (prefix → template → user), documented, tested.
- Two presets with the same id in one file: load error names both lines.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Where presets live | One `presets.yaml`: `~/.config/rex/presets.yaml`, plus `.rex/presets.yaml` in trusted workspaces. |
| D2 | Prompt prefix | Optional `prompt_prefix`. |
| D3 | Wizard order | Presets first, "Custom…" last. |
