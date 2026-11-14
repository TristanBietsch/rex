# U3 Remove all other agents from settings

**Summary**
- Only `claude` and `antigravity` show in the spawn wizard, the settings page and `rex setup`. `echo` stays built in but hidden, for tests only.
- `codex`, `gemini`, `ollama`, `grok`, `deepseek` and `kimi` are deleted from `builtin.yaml`. Before that, a guard lands so that the breakage commit ab92413 fixed for gemini (a partial `tools.yaml` overlay stops the daemon from starting; a saved default spawn tool fails) cannot come back.
- Old sessions, saved settings and `tools.yaml` overlays that name a removed tool keep working, or fall back with a clear message.

**Current behavior**
- `internal/catalog/registry/builtin.yaml`: `echo` (line 7), `claude` (28), `codex` (74), `antigravity` (126, owned by A1), `gemini` (176, hidden), `ollama` (196), `grok` (227, hidden), `deepseek` (250, hidden), `kimi` (276, hidden). The header comment at lines 3-4 promises a "Settings → Onboarding" toggle that does not exist: `SectionOnboarding` is declared at `settings/types.go:26` and no setting uses it.
- `loader.go:93-136` (`mergeOne`) never merges `enabled_by_default`. A hidden built-in tool therefore cannot be re-enabled by the user. Today it is reachable only through `rex new -tool <id>`.
- Every tool list goes through the filter `tui/wizard.go:106` `visibleTools` (it uses `hiddenInWizard{"echo"}` plus `EnabledByDefault`):
  - the spawn wizard (`wizard.go:47`);
  - the settings spawn pickers (`settings.go:36-37`);
  - the `rex setup` defaults step (`setup_wizard.go:182`).
- `loader.go:68-71`: when the merged registry fails validation, `Load` fails. The daemon then refuses to start (`cmd/rex-daemon/main.go:64-68`). The TUI drops the whole user file (`wizard.go:93`). This is the mechanism behind the gemini breakage that ab92413 fixed.
- Restored sessions never touch the registry (`state/persist.go:372` `fromSummary`). The board shows the `ToolID` string.
- Places that hardcode tool ids:
  - `tui/attach.go:84` `altScreenTools`;
  - `cli/session/attach.go:43` `redrawKickTools`;
  - `cli/setup/doctor.go:187` `checkToolBinaries`;
  - `tui/keymap.go:214` `filterTools` (also used by `header.go:109` and `board_mouse.go:171`);
  - `tui/wizard.go:345` `toolShortcodes`;
  - `daemon/server/spawn_prompt.go:30-37`.
- Splash bug: `splash_steps.go:174-183` counts tools with `EnabledByDefault == nil` as hidden, so claude and codex show as "hidden".
- A spawn with an unknown tool fails with a generic error (exit 1): `spawn.go:35-38` returns "tool X/Y not in registry", and it is not marked `badIntent`.

**Design**

*Decision: delete, not hide.* Justification:
1. Hidden built-ins are dead code. Users cannot re-enable them, and the promised toggle does not exist.
2. A user who wants codex or ollama adds a full entry to `tools.yaml`. User tools have `EnabledByDefault == nil`, so they are visible in the wizard and settings automatically. `docs/howto/add-a-tool.md` gets copy-paste entries for codex and ollama, taken from today's YAML.
3. Fewer built-ins means less surface for A1, S and T to reason about.
4. ab92413 kept gemini hidden only because removing it broke two things. The guard below fixes both, and a regression test reproduces the exact ab92413 scenario.

If the reviewer still prefers to keep `gemini` hidden, only step 2 changes (skip deleting lines 176-195). Nothing else in this plan depends on it.

*Guard (lands before any deletion):*
- `registry.retiredToolIDs = {"codex","gemini","ollama","grok","deepseek","kimi"}`.
- In `merge`, an overlay whose id is retired and not in the base list goes through a per-tool check (`validateTool(t)`, extracted from `validate`). If it fails, the entry is dropped and the reason is appended to the new `Registry.Warnings []string`. The message reads: "tool codex is no longer built in; define it fully in tools.yaml (docs/registry.md)".
- An invalid tool with any other id still fails the load, so typos stay loud.
- The daemon logs `Warnings` at boot and on reload (`main.go` after line 69, `boot/reload.go:26`). The splash step `stepRegistryLoad` shows them as `stepWarn`.

*Saved defaults:*
- New `resolveSpawnDefault(store *settings.Store, tools []registry.Tool) (registry.Tool, registry.Model, string, bool)` in a new file `tui/spawn_defaults.go`. It returns the stored tool, model and effort if valid, otherwise the first visible tool with its model's default effort; the bool reports a fallback.
- It is used by `spawnSessionCmd` and `quickSpawnSlug` (`update.go:569-610`) and by `currentSpawnChoice` (`settings.go:303`).
- On fallback, the board shows "default_spawn_tool codex is no longer available; using claude (change it in S)" once per run. `config.yaml` is not rewritten silently.

*Unknown tool on spawn:* `spawn.go:35-38` becomes `badIntent(...)` with the text "tool %q is not in the registry (add it to tools.yaml; see docs/registry.md)". `rex new` then exits 5.

*echo:* add `enabled_by_default: false` to the echo entry and keep `hiddenInWizard`. Fix the header comment.

*Hardcoded maps:*
- **Keep** the removed ids in `altScreenTools`, `redrawKickTools`, `toolShortcodes` and the `spawn_prompt.go` cases. They are inert, they keep attach correct for restored codex/gemini sessions, and they keep prompt delivery working for a codex entry the user adds back. Add a comment saying so.
- `checkToolBinaries` now derives its list from the registry: new `registryToolBinaries()` returns `Command[0]` of the visible tools, deduped. Only the body of `checkToolBinaries` changes; `runAllChecks` is not restructured, because A1 appends `checkAgyVersion()` there.
- `filterTools` becomes a function, `filterToolIDs(m Model) []string`: `"all"` plus the visible registry tool ids (cached on `Model.ToolIDs`, filled by the `registry.load` splash step) plus any tool ids present in `m.Sessions`, so old codex sessions stay filterable. `header.go:109`, `board_mouse.go:171` and `cycleFilter` (`keymap.go:216`) call it. T1's `:filter <tool>` validation should use `filterToolIDs`.
- Fix the splash count to use `visibleTools`.

**Files to change**
- `internal/catalog/registry/builtin.yaml` (lines 1-4 header; echo entry; delete `codex` 74-124, `gemini` 176-195, `ollama` 196-226, `grok` 227-249, `deepseek` 250-275, `kimi` 276-end). The `antigravity` block (about lines 123-174) is not touched.
- `internal/catalog/registry/loader.go`: `merge`, `validate` → `validateTool`, `Registry.Warnings`, `retiredToolIDs`.
- `internal/catalog/registry/loader_test.go` (`TestLoad_RealToolsPresent`, `TestLoad_OptInDefaults`), `trust_test.go` (only the gemini assertions at lines 83-86).
- New `testdata/tools-retired-partial.yaml` and `testdata/tools-retired-full.yaml`.
- `cmd/rex-daemon/main.go` (log warnings after line 69), `internal/daemon/boot/reload.go`.
- `internal/daemon/server/spawn.go` (lines 35-38).
- `internal/surface/tui/spawn_defaults.go` (new), `update.go` (`spawnSessionCmd`, `quickSpawnSlug`), `settings.go` (only the body of `currentSpawnChoice`), `splash_steps.go` (`stepRegistryLoad`), `keymap.go` (`filterTools` → `filterToolIDs`), `header.go:109`, `board_mouse.go:171`, `model.go` (`ToolIDs` field).
- `internal/surface/cli/setup/doctor.go` (only the body of `checkToolBinaries`, plus a new `checkRegistryWarnings` appended in `runAllChecks` after A1's row).

**Test plan (test-first)**
- `registry`:
  - `TestBuiltinToolIDs`: ids are exactly `[echo claude antigravity]`, and echo has `EnabledByDefault == false`.
  - `TestLoad_RetiredPartialOverlayDropped`: a partial `gemini` overlay (the ab92413 regression) and a partial `codex` overlay load fine, the tool is absent, and `Warnings` names it.
  - `TestLoad_RetiredFullOverlayLoads`: a full codex entry is present.
  - `TestLoad_UnknownPartialOverlayStillFails`.
- `tui`:
  - `TestVisibleTools_ClaudeAndAntigravityOnly`.
  - `TestResolveSpawnDefault_RetiredToolFallsBack` (stored `gemini/2.5-pro` → `claude` with its default model and effort, fellBack=true).
  - `TestQuickSpawn_RetiredDefaultSpawnsClaude` (fake `sessionSpawner`).
  - `TestFilterToolIDs_IncludesSessionTools`.
  - `TestStepRegistryLoad_CountsVisible`.
  - The existing `TestSpawnPicker_*` tests keep passing (the +1 step now cycles to antigravity).
- `server`: `TestNewSession_UnknownToolIsBadIntent`.
- `setup`: `TestCheckToolBinaries_FromRegistry`, `TestDoctor_WarnsOnDroppedOverlay`.
- e2e: no change (it uses echo explicitly).

**Docs/CHANGELOG**
- `docs/registry.md`: "Built-in tools" section, a new "Retired tools" rule (merge rule 7), and removal of the "enabled_by_default … Settings" text.
- `docs/howto/add-a-tool.md`: full codex and ollama entries.
- Mentions in `docs/cli.md` (the attach paragraph), `docs/tui.md`, `docs/architecture.md`, `docs/agents/modules/registry.md` and `docs/agents/modules/pty.md`.
- CHANGELOG `[Unreleased]` → **Removed**: the built-in codex, gemini, ollama, grok, deepseek and kimi tools, with migration notes.

**Conflict map**
- A1: owns the `antigravity` block and its trust_args. U3 only deletes other blocks and touches the gemini lines of `trust_test.go` `TestBuiltinAntigravity` (lines 83-86). A1 should rebase on U3 step 2, or U3 rebases on A1; either way this is a trivial hunk-level merge.
- T1-T5: `keymap.go` `filterTools`/`cycleFilter` and `header.go:109`. U3 lands `filterToolIDs` first and T1 consumes it. T4 (resume) must handle "tool not in registry" for old sessions, using the same `badIntent` message.
- S1-S5: the summarizer uses Ollama over HTTP, not the `ollama` tool, so there is no conflict. If the master agent wants a non-claude tool, it must be a `tools.yaml` entry.
- U1/U2: the settings restyle runs last. U3 changes only the logic of `currentSpawnChoice`.
- P1: also touches `builtin.yaml` (adds `skill_args` to the claude entry). Different lines.

**Security notes**
- Fewer spawnable programs by default. Dropping retired overlays happens only for the fixed id set; it never weakens validation for other ids.
- Retired-tool warnings are written to logs and to the TUI through the existing sanitizers. A tool id comes from the user's own file.

**Open questions for user**
- None blocking. Optional: keep `gemini` hidden rather than deleted. **Recommended default: delete it, with the guard.**

**Ordered steps (each one commit, `make check` green)**
1. `feat(registry): drop partial overlays of retired tools with a warning`: guard, `Warnings`, daemon and splash logging, tests (built-ins are unchanged at this point).
2. `feat(registry)!: ship only claude and antigravity (echo hidden)`: YAML deletions, echo hidden, updated registry tests, docs, CHANGELOG.
3. `fix(tui): fall back when the quick-spawn default tool is gone`: `resolveSpawnDefault`, splash count fix.
4. `refactor(tui): derive filter chips from the registry and sessions`: `filterToolIDs`.
5. `fix(daemon): unknown tool on spawn is bad_intent with a hint`.
6. `feat(doctor): check tool binaries and overlay warnings from the registry`.

**Size**: M
