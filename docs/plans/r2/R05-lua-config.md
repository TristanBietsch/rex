# R05 Settings in Lua (`config.lua`) on top of U2

## Summary
- Implements the Lua half of U2 that D1 deferred: a `~/.config/rex/config.lua` evaluated by every rex process that reads settings (TUI, daemon, CLI), in a fresh, sandboxed, time-boxed gopher-lua state with a settings-only API.
- YAML stays: `config.yaml` remains the TUI's write target and the fallback. Lua values win.
- Same evaluator later reads `<workspace>/.rex/config.lua` (R06).

## Current behavior (after round 1)
- `settings.Store` loads `config.yaml` once per process; U2-2 made `Save` sparse (dirty keys only, read-modify-write); U2-8 adds `SettingsHub` (poll + SIGHUP + `rex reload` + `:reload`).
- `internal/features/lua` runs `init.lua` in the daemon only (`rex.on/send/list/log`; 5 s load, 3 s handler; libs base, table, string, math, package). It is trusted code, not a sandbox.
- `lua_config_path` is a dead read-only row; U1 removes or repurposes it.

## Design

### Evaluator (`internal/catalog/settings/luacfg`)
- `Eval(ctx, path string, base map[string]any) (Result, error)`; `Result{Values map[string]any, Themes []theme.Palette, Locked []string, Source string}`.
- Fresh `LState` per eval; libs: base (minus `dofile`, `loadfile`, `load` with strings), table, string, math. No `os`, `io`, `package` (Decision D2 controls `require` of sibling files under `~/.config/rex/lua/`).
- Limits: 250 ms wall clock via context, 10 MiB allocation cap via instruction-count hook, script size 256 KiB.
- API shape per Decision D1. Example for (a):
  ```lua
  rex.set("theme", "tokyo_night")
  rex.set("auto_trust_dirs", { "~/dev" })
  if os_name == "darwin" then rex.set("screensaver_keep_awake", true) end
  rex.theme("mine", { accent = "#B392F0", bg = "#0F1115" })
  ```
- Values pass through `Setting.Normalize` (round-1 P3-H1) so Lua cannot set what `rex config set` could not. Unknown keys: warning, ignored.

### Precedence and writes
- Registry defaults < `config.yaml` < `config.lua` (< `.rex/config.lua` in R06) < CLI flags.
- Rows set by Lua are **locked** in the TUI (lock glyph + "set in config.lua:12"). Editing a locked row per Decision D3.
- `rex config set` on a locked key exits `ExitInvalidArgs` (5) with the file:line (U2 test already reserves this).

### Errors
- Syntax/runtime error or timeout: keep the last good values, show one notice in the TUI header and in `rex doctor`. Never fall back silently to defaults.

## Feature flag
- `lua_config` (on; inert without a `config.lua`). Off = file ignored, rows unlocked, no evaluator runs in any process.
- Lives in Settings → Features (roadmap "Feature flags"). Its owning step adds an **off-is-inert** test: with the flag off, no goroutine/subprocess/network/file write happens and the feature's keys and commands answer "off: enable in Settings → Features".
- Flag lands in step R05-3.

## Plan
1. **R05-1 Evaluator core.** Sandbox, limits, `rex.set`. Tests: `TestEvalSet`, `TestEvalTimeout` (infinite loop returns within 300 ms), `TestEvalNoOS` (`os.execute` is nil), `TestEvalNormalize` (bad `auto_trust_dirs` rejected with the same message as `rex config set`), fuzz `FuzzEval` on random scripts (no panic).
2. **R05-2 Themes from Lua.** `rex.theme(name, tbl)` → `theme.Palette` via U2's YAML validator. Tests: missing token falls back to default's token with a warning.
3. **R05-3 Store layer.** `Store.Load` runs Eval after YAML; records `Locked` + source line. Tests: precedence table; sparse Save never writes Lua-owned keys to YAML.
4. **R05-4 SettingsHub watches `config.lua`** (+ `lua/` if D2 = a). Tests: edit file → TUI message within one poll; bad edit keeps last good values.
5. **R05-5 TUI lock UI.** Lock glyph, inspector shows file:line, edit behavior per D3. Golden update.
6. **R05-6 CLI.** `rex config get` prints source; `rex config edit --lua` opens config.lua (creates a commented template on first use); `rex config lua-export` writes current YAML values as a starter `config.lua`.
7. **R05-7 Docs + SECURITY.md.** New execution point in every rex process; same trust level as `init.lua`; list of removed libs.

## Review focus
- `config.lua` owned by another user or world-writable: refuse to evaluate (same check as `init.lua`), notice says why.
- Script that sets 10,000 keys or builds a huge string: hits memory cap, not OOM.
- Daemon and TUI evaluate the same file at different times mid-edit: both converge on the next poll.
- `rex --help` must stay fast with a slow `config.lua`: CLI paths that only need colors cap eval at 50 ms then use YAML values.
- Windows paths in `rex.set("auto_trust_dirs", {"C:\\dev"})`.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Lua API shape | Imperative `rex.set(key, value)` + `rex.theme(name, tbl)`. |
| D2 | Modules | `require` allowed only from `~/.config/rex/lua/`. |
| D3 | Editing a locked row | Refuse with "set in config.lua:N — press e to open". |
| D4 | Long-term YAML | Keep `config.yaml` as the TUI write target. |
