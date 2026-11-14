# P1 Name, model and skills in the new-session wizard

> **Revised 2026-11-11:** the wizard's name step (`wizName`, ordered step 6) was built and then removed in `0bf135f` because it added friction to every spawn. The wizard now goes effort → skills → describe, and the slug comes from the task alone. A session is named with `r` on the board or `rex new -name`; `NewSession.Name` and the CLI flag are unchanged. The sections below are updated to match; struck items record what was dropped.

**Summary**
- The new-session wizard (`n`, `:new`) gains two steps: **model** (today it silently picks the first model) and **skills** (Claude Code skills injected for this session only). `rex new` gets `-name` (the display name) and `-skill`. ~~A **name** step in the wizard~~ was dropped (see the revision note).
- Skills come from a rex-owned library, `~/.config/rex/skills/<name>/SKILL.md`. The daemon copies the chosen ones into a per-session plugin under the session's own folder and starts Claude with `--plugin-dir`. Nothing is written to `~/.claude`.
- Antigravity has no session-only plugin flag, so it degrades cleanly: the skills step is skipped, and `-skill` with agy exits 5 with a clear message.

**Current behavior**
- `tui/wizard.go:18-30`: the steps are provider → effort → describe. "The model selection step … is gone — the wizard auto-picks the first model" (`firstModel`, lines 120-131). There is no name step; the slug is derived from the task (`wizard.go:259`).
- `protocol.NewSession` (`wire/protocol/intents.go:65-75`) has no `Name` and no `Skills`. A name can only be set afterwards with `SetSessionName` (`intents.go:135`). `state.Session.Name` exists (`state/session.go:43`).
- `rex setup` is one-time global config. It already has a tool/model/effort step (`setup_wizard.go:464`), but per-agent name and skills make no sense there. "Setup" in this TODO therefore means the per-agent spawn wizard: "name", "choose model" and skills "to inject into it" all describe the agent being created.
- Claude spawn argv today: command + model args + effort + `trustArgsFor` + `adapter.ClaudeHookArgs()` (`--settings <json>`, `adapter/claude_hooks.go:112-133`) + `-- <prompt>` (`spawn_prompt.go:30-32`).
- Verified in the installed Claude Code 2.1.286 binary:
  - help text: `--plugin-dir <path>  Load a plugin from a directory or .zip for this session only; a folder of plugins loads each child (repeatable)`;
  - the strings `.claude-plugin/plugin.json` and `SKILL.md` are present.
- agy (`~/.local/bin/agy`): has no `--plugin-dir` or similar flag. Its skills live in `<workspace>/.agents/skills/<name>/` or `~/.gemini/config/skills/<name>/`. Using them means writing into the user's repo or global config, so that is ruled out.

**Design**

*Wizard (`wizard.go`).* Steps: `wizProvider → wizModel → wizEffort → wizSkills → wizDescribe`.
- `firstModel()` is replaced by `selectedModel()` backed by a new `ModelIdx`.
- `wizModel` is skipped when the tool has one model. Its default is `default_spawn_model` when the tool equals `default_spawn_tool`, else index 0.
- ~~`wizName`: optional free text, validated live with `protocol.NormalizeSessionName`.~~ Removed: it added friction to every spawn. Name a session with `r` on the board instead.
- `wizSkills`: a multi-select with space to toggle and enter for next. It shows each skill's description, scrolls with the existing `windowLines` helper, and keeps the fixed `wizardBodyLines`. It is skipped when the tool has no `skill_args` or the library is empty.
- Slug: `deriveAgentSlug(tool, model, task)`. An empty task gives a hash fragment.
- Rendering stays inside the `renderWizard` switch so U1/U2 can restyle it.

*Registry.*
- New field `Tool.SkillArgs []string \`yaml:"skill_args"\`` with placeholder `{plugin_dir}`.
- `validateTool` requires the placeholder when the field is set. `mergeOne` replaces the value when an overlay sets it, like `trust_args`.
- The built-in claude entry gets `skill_args: ["--plugin-dir", "{plugin_dir}"]`.

*Skill library: new package `internal/features/skills`.*
- `paths.Layout.SkillsDir = ~/.config/rex/skills`.
- `List(dir) ([]Skill, []error)`: reads `<name>/SKILL.md` frontmatter (`name`, `description`). It skips symlinked folders and names that fail `ValidName` (`^[a-z0-9][a-z0-9-]{0,63}$`).
- `Stage(lib, sessionDir string, names []string) (string, error)`:
  - creates `<sessionDir>/plugin` (0700);
  - writes `.claude-plugin/plugin.json` with `{"name":"rex","description":"skills rex added to this session","version":"1"}`;
  - copies each `<lib>/<name>/` tree into `skills/<name>/` through `os.OpenRoot` (no traversal), regular files only (no symlinks, checked with `Lstat`), with caps of 256 files and 4 MiB total.
- Copying, not symlinking, snapshots the skills for the session's lifetime. The session's directory removal (`rm`, `prune`, retire) cleans the copy up.

*Wire and daemon.*
- `NewSession` gains `Name string \`json:"name,omitempty"\`` and `Skills []string \`json:"skills,omitempty"\``. `SessionSummary` gains `Skills`. `state.Session` gains `Skills`, which is persisted in `meta.json`.
- `normalizeNewSession` (`validate.go:88`) runs `NormalizeSessionName`.
- `validateNewSession` checks skills: at most 16, unique, valid names.
- In `handleNewSession`, after the tool is found:
  - the tool has no `SkillArgs` → `badIntent("tool %s cannot load skills per session")`;
  - a skill is missing from the library → `badIntent`;
  - otherwise `Stage`, then append the expanded `SkillArgs` before the hook args and before the `--` prompt.
- `server.Config` gains `SkillsDir`, set by `cmd/rex-daemon` with a `-skills-dir` flag for tests.

*Old daemon compatibility.* An old daemon drops the unknown fields silently. `rex new` and the TUI compare the `SessionAdded` summary: skills requested but `Skills` missing → warn "this rex-daemon ignored skills; run `rex daemon restart`". `rex new` applies the same check to `-name`; the TUI checks skills only, since the wizard sends no name.

*CLI.*
- `rex new -name "text" -skill a -skill b` (a repeatable flag type in `core`).
- New `rex skills` command that lists the library (name and description).
- Only the `run()` switch in `main.go` gets a `case "skills"`.

*agy.* No `skill_args`, so the skills step is hidden and the CLI gets exit 5. A1 investigates whether agy's session-only `--settings` JSON can name a skills path. If it can, A1 adds `skill_args` to the agy entry later, with no code change needed.

**Files to change**
- `internal/catalog/registry/types.go`, `loader.go` (`validateTool`, `mergeOne`), `builtin.yaml` (claude entry only, after its `trust_prompt`).
- `internal/runtime/paths/paths.go` (`SkillsDir`).
- New `internal/features/skills/{skills.go,stage.go,*_test.go}`.
- `internal/wire/protocol/intents.go` (`NewSession`), `events.go` (`SessionSummary`).
- `internal/daemon/state/session.go`, `persist.go` (`WriteMeta`, `fromSummary`).
- `internal/daemon/server/validate.go`, `spawn.go` (between the `trustArgsFor` line and the hook block), `server.go` (`Config.SkillsDir`).
- `cmd/rex-daemon/main.go` (flag).
- `internal/surface/cli/session/new.go`, new `internal/surface/cli/session/skills.go`, `cmd/rex/main.go` (one case).
- `internal/surface/tui/wizard.go` (`WizardState`, the step functions, `renderWizard`, `wizardFooter`, `wizardLaunchCmd` signature).

**Test plan (test-first)**
- `registry`: `TestValidateSkillArgs` (the placeholder is required), `TestMergeSkillArgs`, `TestBuiltinClaudeSkillArgs`, `TestBuiltinAntigravityHasNoSkillArgs`.
- `skills`:
  - `TestList` (frontmatter parsed; bad names and symlinked folders skipped);
  - `TestStage_CopiesSelected` (checks the layout and `plugin.json`);
  - `TestStage_RejectsTraversalName` (`../x`, `a/b`);
  - `TestStage_RefusesSymlinkInsideSkill`;
  - `TestStage_SizeCap`;
  - `FuzzValidName`.
- `server`:
  - `TestNewSession_SkillsAddPluginDir`: a test registry with an echo-based tool that has `skill_args: ["--plugin-dir","{plugin_dir}"]`; check the supervisor argv and that the folder exists under the session dir;
  - `TestNewSession_SkillsUnsupportedToolIsBadIntent`;
  - `TestNewSession_UnknownSkillIsBadIntent`;
  - `TestNewSession_NameNormalized` / `_NameTooLongRejected`;
  - `meta.json` round-trip for `Skills`.
- `cli/session`: `TestParseNewArgs_NameAndSkills`, plus an e2e txtar `session_new_skills.txtar` (echo tool overlay with `skill_args` in the testscript `tools.yaml`, `-skill` against a skills dir under `$HOME`).
- `tui`:
  - `TestWizard_ModelStepSkippedForSingleModel`;
  - `TestWizard_ModelDefaultsToSpawnDefault`;
  - `TestWizard_NoNameStep` (replaced the name-step tests);
  - `TestWizard_SkillsStepHiddenForAgy`;
  - `TestWizard_SkillToggleSendsSkills` (checks the payload through the fake client);
  - snapshot tests for the new steps.

**Docs/CHANGELOG**
- `docs/registry.md` (`skill_args`), `docs/tui.md` (wizard steps), `docs/cli.md` (`rex new -name -skill`, `rex skills`), `docs/protocol.md` (the new `NewSession` and summary fields), `docs/paths.md` (`skills/`), the `docs/agents` module docs.
- New `docs/howto/skills.md`: library layout and a SKILL.md example.
- CHANGELOG **Added**.

**Conflict map**
- U3: both touch `builtin.yaml` (U3 deletes other tools, P1 adds lines in the claude entry) and `loader.go` (U3 `merge`/`validateTool` split; P1 adds a field to it). Land U3 step 1 first; P1 reuses `validateTool`.
- P2: both touch the wizard (P2 adds a trust line in the describe step) and `NewSession` (P2 adds `TrustFolder`). Separate fields and functions; land P1's step refactor first.
- P3: `new.go` parsing refactor (`parseNewArgs`, `core.ParseArgs`) lands first; P1 adds flags to it. `main.go`: P1 adds one `case`.
- T4 (resume): a resumed session should reuse `sessions/<id>/plugin` if it exists. T4 reads `Session.Skills`.
- A1: owns agy. Decides on a later `skill_args` for agy and on refreshing models from `agy models` (see the open questions).
- U1/U2: restyle the wizard last; the logic stays in the step functions.
- S (master agent): may pass `Skills` in `NewSession`; it is the same API.

**Security notes**
- Skills are instructions the agent follows. The library is user-owned under `~/.config/rex`, the same trust level as `tools.yaml` and `init.lua`, which SECURITY.md lists as out of scope.
- The daemon never trusts client paths. It takes names only, validates them, and resolves them under its own `SkillsDir` through `os.Root`.
- Copies are limited to regular files with size and count caps. The plugin folder is 0700 inside the session dir.
- No writes to `~/.claude` or `~/.gemini`.
- Only `skills/` subtrees are copied and rex writes its own `plugin.json`, so library content cannot bring in hooks or MCP servers. `--plugin-dir-no-mcp` also exists in the binary and could be used as extra hardening (to be verified).

**Open questions for user**
1. Library location: rex-owned `~/.config/rex/skills/`. Skills in `~/.claude/skills` already load globally, so listing them would add nothing. **Recommended: rex-owned only.**
2. Should quick-spawn `i` also take default skills (a new `default_spawn_skills` setting)? **Recommended: no, for now.**
3. agy model slugs depend on the account (A1 note). Should the model step refresh the list from `agy models` at wizard open? **Recommended: not in P1.** Show the registry models; A1 can add a cached refresh later.
4. Six steps is long. (Now five: the name step was dropped.) **Recommended:** keep separate steps that skip themselves automatically (single model, no effort, no skills support, empty library) so the common path stays at 3-4 Enter presses.

**Ordered steps (each one commit, `make check` green)**
1. `feat(registry): skill_args with {plugin_dir}; claude uses --plugin-dir`.
2. `feat(skills): skill library listing and per-session plugin staging` (+ `paths.SkillsDir`).
3. `feat(protocol,daemon): NewSession name and skills` (validation, staging, argv, meta persistence, `-skills-dir`).
4. `feat(cli): rex new -name/-skill and rex skills` (+ e2e txtar, warning on old daemons).
5. `feat(tui): wizard model step` (the `selectedModel` refactor).
6. ~~`feat(tui): wizard name step`.~~ Reverted by `0bf135f`.
7. `feat(tui): wizard skills step`.
8. `docs: skills howto, wizard, protocol, CHANGELOG`.

Manual check before merge, done by a human, not in CI: spawn claude with one skill and confirm it appears as `rex:<skill>` in `/skills`.

**Size**: L
