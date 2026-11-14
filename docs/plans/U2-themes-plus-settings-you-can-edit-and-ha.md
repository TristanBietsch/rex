# U2 Themes, plus settings you can edit and hack in Lua config files

## Summary
- Add a `theme` setting (renamed from `color_scheme`, with an alias). It covers U1's built-in themes, an opt-in `auto` (light or dark terminal), and user themes as **declarative YAML files** in `~/.config/rex/themes/`. These files are data, so they are safe to share.
- Recommended architecture (**USER DECISION**): a separate `~/.config/rex/config.lua`, evaluated in every rex process by a small sandboxed, timeout-bounded gopher-lua evaluator. It can only set settings and define themes, and its values win over config.yaml; those rows show as locked in the TUI. `init.lua` stays daemon-only for hooks.
- Add one general settings reload path. It polls config.yaml, config.lua, `lua/` and `themes/`, and is also triggered by SIGHUP, `rex reload` and `:reload`. It feeds a TUI message and a daemon `SettingsHub`, and replaces S3's `boot.WatchSummarySettings`. A read-modify-write, sparse `Store.Save` fixes today's lost-update bug.

## Current behavior (file:line)
- **Themes are hard-coded.** `palettes` (`internal/surface/tui/styles.go:20-62`) is selected by the enum `color_scheme` with options `default/noir/paper` (`internal/catalog/settings/registry.go:12-16`). It is applied at boot (`splash_steps.go:110-122`) and live (`settings.go:388-392`). Users cannot define their own theme.
- **Settings load once per process** through `settings.LoadDefault()` (`store.go:29-37`). Callers: TUI boot (`splash_steps.go:94-108`), daemon (`cmd/rex-daemon/main.go:115-129`, read once), `boot/lua.go:105`, `boot/trust.go:17` (re-read on every spawn), and the CLI (`setup/config.go:39-45`).
  - A running TUI never sees `rex config set`.
  - Daemon summary settings need a restart (`docs/daemon.md`). S3 plans an ad-hoc mtime watcher just for `summary_detail`.
- **Lost update.** `Store.Save` marshals *all* values, defaults included (`store.go:78-107`). The TUI saves on every change and on close (`settings.go:52-62`, `:236`), which overwrites concurrent `rex config set` edits.
  - Also, once a user opens settings, every default is frozen into config.yaml, so later changes to registry defaults never reach them.
- **Lua.** `internal/features/lua` runs `init.lua` in the daemon only (`boot/lua.go:27-75`) with the `rex.on/send/list/log` API. Its timeouts: 5 s load, 3 s handler (`defaults.go`). Libraries: base, table, string, math, package (`runtime.go:107-125`). It is "trusted user code, not a sandbox" (SECURITY.md, Lua README). There is no settings API.
  - `lua_config_path` is a read-only setting (`registry.go:178`).
  - `rex config edit` opens init.lua (`setup/config.go:139`).
  - `rex reload` and SIGHUP reload tools.yaml only (`boot/reload.go:17-43`).
- **Terminal background.** Nothing detects it. lipgloss already honors `NO_COLOR`/`CLICOLOR` through `EnvColorProfile` (lipgloss `renderer.go:74`).

## Design
**A. Architecture decision (USER DECISION). Options weighed:**

| | (a) YAML/TOML theme and settings files only; Lua stays in the daemon | (b) Client-side sandboxed Lua returning settings and themes | (c) Daemon evaluates init.lua and serves resolved settings over the wire |
|---|---|---|---|
| Hackability | Data only, no logic (no `if dark then`) | Full: logic, `require` modules, computed themes | Full, but only while the daemon is up |
| Works without daemon (splash theme before dial, `rex setup`, `rex --help`, `doctor`) | yes | yes | **no**: splash applies the theme before `socket.resolve`/`daemon` (`splash_steps.go:209`) |
| Startup latency | about 0 | LState plus a tiny script, under 2 ms (benchmarked); hard cap 1 s | a socket round-trip, plus daemon start when it is down |
| Hot reload | poll files | poll files; re-evaluate in a fresh LState | daemon pushes an event; needs a protocol change and version-skew handling |
| Security | no code execution; validate data | new code-execution point in every rex process (same trust as init.lua); mitigations below | one execution point, but the daemon becomes a config server; protocol attack surface |
| Windows | fine | fine (pure Go) | fine |
| Cost | M | L | L+ (wire, protocol docs, skew) |

**Recommendation: a hybrid of (a) and (b).**
- Theme files are **YAML data**, safe to download and share.
- Settings logic lives in a **separate `config.lua`** with a settings-only API (no `rex.on/send/list`), evaluated in a fresh, sandboxed, time-boxed LState by each process that needs settings (TUI, daemon, `rex config`, colored CLI output).
- (c) is rejected: it does not work before the daemon is up, it adds protocol churn, and it adds client/daemon version skew.
- Fallback if the user wants less: (a) alone, which is steps 1–3 and 7–9 below without Lua. Size M.

**B. Themes.**
- **Setting.** `theme` (TypeString, default `default`) replaces `color_scheme`.
  - `Setting.Aliases []string` is a general rename mechanism: `Store.Load` maps `color_scheme` to `theme`, and the next Save writes `theme`.
  - The value is either `auto` or a theme name.
  - TUI cycling options come from a dynamic provider, generalized from `spawnPickerOptions` into `dynamicOptions(m, id)`: built-ins, then user files, then Lua-defined themes.
  - An unknown name falls back to `default`, with a notice ("theme 'x' not found").
- **Built-ins** (from U1): `default`, `classic`, `noir`, `paper`, `high-contrast`, `ansi`.
- **`auto`:** `default` on a dark terminal, `paper` on a light one.
  - Detection order: `COLORFGBG`, then termenv's OSC 11/DA1 query via `lipgloss.HasDarkBackground()`.
  - It runs **only when theme=auto**, synchronously in `tui.Run` before `tea.NewProgram`. It must never run after Bubble Tea owns stdin, because a stray reader would eat keys.
  - The result is passed into the Model, and the CLI skips the query.
- **User theme file** `~/.config/rex/themes/<name>.yaml`:
```yaml
extends: noir          # optional; default "default"; chain depth ≤ 4, cycles rejected
dark: true
colors:  { accent: "#FF7A59", needs: "#F2B84B", fleet: ["#..", "..."] }   # keys = U1 tokens, snake_case
glyphs:  { needs: "!", done: "✓", failed: "x", crashed: "o", queued: ".", select: ">" }
```
  - Colors must be `#RGB`, `#RRGGBB`, `"0"`–`"255"` or `""`.
  - Glyphs must be one printable grapheme of cell width 1–2, with no C0, C1, ESC or bidi controls.
  - The file is capped at 64 KiB, with at most 64 theme files. Unknown keys produce warnings, not errors.
  - Glyphs extend U1's prebuilt marker styles; `stateMarkerCellFor` reads `curGlyphs`.
- **CLI.**
  - `rex theme list`: name, source (builtin, file or lua), current marker, and a swatch row.
  - `rex theme show <name>`: token table plus a sample board line.
  - `rex theme new <name> [-from default] [-force]`: writes a commented YAML skeleton, mode 0600.
- **TUI.** `:theme <name>` sets the theme (previewed live and saved); `:theme` alone cycles.

**C. config.lua (settings and themes in Lua).**
- **Paths** (`internal/runtime/paths`): `LuaConfigFile()` = `~/.config/rex/config.lua`, `LuaModuleDir()` = `~/.config/rex/lua/`, `ThemesDir()` = `~/.config/rex/themes/`. All are fixed paths, never a setting, to avoid chicken-and-egg loading.
- **Evaluator** `internal/features/lua/config.go`:
```go
type ConfigEnv struct{ Process string; Platform string; DarkBackground *bool; Current map[string]any; Getenv func(string) string }
type ConfigResult struct{ Settings map[string]any; SetAt map[string]string /* "config.lua:12" */; Themes map[string]map[string]any; Warnings []string }
func EvalConfig(ctx context.Context, path string, env ConfigEnv) (ConfigResult, error) // DefaultConfigTimeout = 1s
```
  - A fresh LState per evaluation (`SkipOpenLibs`). Libraries: base, table, string, math, and package with `package.path` set to `LuaModuleDir()/?.lua` only. `package.cpath` and loaders beyond Lua files are cleared.
  - `dofile`, `loadfile`, `load` and `loadstring` are removed. Modularity comes through `require` from `lua/` only.
  - `CallStackSize: 200`, `RegistryMaxSize` bounded. The timeout is set with `L.SetContext`.
  - API:
    - `rex.set(id, value)` and `rex.opt.<id> = value` (metatable sugar);
    - `rex.get(id)`: the effective value before Lua;
    - `rex.theme(name, {extends=…, colors={…}, glyphs={…}})`;
    - `rex.platform`, `rex.process` ("tui", "cli" or "daemon"), `rex.dark_background` (bool or nil);
    - `rex.getenv(name)`, `rex.log(level, msg)`.
  - Each `rex.set` is validated through an exported `settings.Validate` (wraps `coerce`). Bad ids or values become warnings that record `L.Where(1)`, and evaluation continues.
  - **All-or-nothing on a Lua error or timeout:** the overlay and Lua themes are discarded, and YAML plus defaults apply, with a reported error. The result stays predictable.
- **Composition** `internal/catalog/userconfig` (new):
  - `Load(opts) (*Config, error)` = defaults, then config.yaml, then themes/*.yaml, then config.lua (only if it passes `CheckPrivate`). It returns `Config{Store, Themes, Report{LuaErr, Warnings, Stamp}}`.
  - `Stamp()` is the size and mtime of config.yaml, config.lua, every file in `lua/` and `themes/`, and those directories.
  - The package imports catalog/settings, catalog/theme and features/lua. STRUCTURE.md gets a row for it.
- **Provenance in `settings.Store`.**
  - `src map[string]Source` with values Default, File and Lua; `Overlay(map, SourceLua, setAt)`; `Source(id)`, `SetAt(id)`.
  - `Set` on a Lua-sourced id returns `ErrManagedByLua{At: "config.lua:12"}`.
  - **Save becomes sparse read-modify-write:** re-read the file, apply only this Store's *dirty* ids (Set writes the key; Reset deletes it), write atomically, clear dirty. Lua values are never persisted.
  - This fixes the lost update, and new registry defaults now reach users who never changed a key.
- **Precedence: Lua over YAML (USER DECISION).**
  - In the TUI settings page, Lua-managed rows show a `lua` badge plus `config.lua:12` in U1's detail pane, and ←/→ explains instead of changing the value.
  - `rex config set` on such a key exits with ExitInvalidArgs: "theme is set by ~/.config/rex/config.lua:12".
  - `rex config list` gains a SOURCE column, and `-json` gains a `source` field.
- **`rex config edit [lua|hooks|yaml]`:** the default target is config.lua, created from an embedded `example_config.lua` template. `hooks` opens init.lua, and `yaml` opens config.yaml.
- **Who evaluates config.lua.** The TUI (new splash boot step `config.lua`, shown as OK, WARN or SKIP, between `settings.load` and `theme.apply`), the daemon, `rex config`, `rex theme` and `rex setup`, and CLI commands that print styled output (U1's `core.Styles()`).
  - `rex complete`/completion and every `-json` path do **not** load config.lua: shell completion stays fast and has no side effects.

**D. General settings reload path (replaces S3's watcher).**
- **TUI.**
  - `configTick` every 2 s (`tea.Tick`) runs a Cmd that compares `userconfig.Stamp()`. When it changed, `userconfig.Load` runs in the Cmd goroutine (each LState is private, so this is safe) and returns `configReloadedMsg{cfg, changedIDs}`.
  - `Update` swaps `m.Store`/`m.Themes`, calls `applyTheme` on the UI goroutine (U1's fix), and calls `applyLive` for each changed id (audio, mouse, theme, max_concurrent).
  - On a Lua error it keeps the previous config and shows "config.lua: <err> (kept previous)" in the status line.
  - `:reload` triggers this immediately and still reloads the daemon registry.
  - The TUI's own Save also triggers a reload. The diff is empty, so it is a no-op.
- **Daemon.** New `boot.SettingsHub` (`internal/daemon/boot/settings.go`):
  - `Current() *userconfig.Config` (atomic pointer);
  - `Subscribe(ids []string, fn func(old, new *userconfig.Config))`;
  - `Reload() error`;
  - `Watch(ctx, 5*time.Second)`.
  - `main.go` creates it once and replaces its direct `settings.LoadDefault`.
  - `OnReload` (Reload intent) and `ReloadOnHUP` call `ReloadRegistry` and then `hub.Reload()`.
  - Subscribers:
    - summarizer `SetDetail` (S3's `summary_detail`; S3's `summary_settings.go` is deleted and its test moves to a hub subscriber test);
    - the retire default TTL, if the server exposes a setter (otherwise unchanged);
    - `AutoTrustDirs` reads `hub.Current()` instead of re-reading disk per spawn;
    - `luaConfigPath` reads `hub.Current()`.
  - init.lua hooks are **not** re-run on reload (out of scope; documented).
- **Polling, not fsnotify.** It adds no dependency, behaves the same on macOS, Linux and Windows, and copes with editors that save by rename, because it stats paths rather than inodes.

**E. "More settings modular" in practice.**
- config.lua can `require("rex/appearance")` and similar files from `lua/`.
- Themes are one file each and can `extends` another.
- Every registry setting (including T1's `board_hidden_sections` and S3, S4 and S5 toggles) becomes Lua-settable for free, because the overlay goes through the registry.
- Keybinding remap (`rex.map("board","x",":rm")` on top of U1's `commands`/`bindings` tables) is deliberately left for a follow-up (open question 6).

## Files to change
- New:
  - `internal/catalog/theme/spec.go` (Spec, LoadDir, Resolve with `extends`, validation, glyphs) plus tests;
  - `internal/features/lua/{config.go,config_test.go,example_config.lua}`;
  - `internal/catalog/userconfig/{userconfig.go,watch.go,private_unix.go,private_windows.go}` plus tests;
  - `internal/daemon/boot/settings.go` plus tests;
  - `internal/surface/cli/setup/theme.go` (`rex theme`) plus tests;
  - `internal/surface/tui/config_reload.go`;
  - `docs/howto/themes.md`, `docs/howto/lua-config.md`.
- Modified:
  - `internal/runtime/paths/*.go` (3 accessors; keep 100% coverage);
  - `internal/catalog/settings/{types.go,registry.go,store.go}` (Aliases, Source, dirty tracking, sparse Save, `Validate`; `theme` replaces `color_scheme`; `lua_config_path` help);
  - `internal/surface/tui/{styles.go,splash_steps.go,update.go,settings.go,command.go,tui.go,board.go}` (glyph tokens, dynamic options, lock badge, `:theme`, tick, auto detection in `Run`);
  - `setup_wizard.go` (applies the user's theme instead of the `init()` default);
  - `internal/surface/cli/setup/config.go` (source column; error on managed keys; `edit` targets);
  - `cmd/rex/main.go` (`theme` verb), `cli/core/help.go`, `cli/meta/completion.go`;
  - `cmd/rex-daemon/main.go`, `internal/daemon/boot/{lua.go,trust.go,reload.go}`;
  - `tools/coverage/baseline.txt` (userconfig, theme floors).

## Test plan (test first)
- **Store:**
  - `TestSaveIsReadModifyWrite`: two Stores load, A sets `spinner`, B sets `soundset`, both Save, and the file has both.
  - `TestSaveSparse`: a fresh Store with one Set writes one key; Reset deletes it.
  - `TestLuaOverlayNeverPersisted`.
  - `TestAliasColorScheme` (old file with `color_scheme: noir` gives `theme=noir`; Save writes `theme`).
  - `TestSetManagedByLuaErrors`.
  - Extend the fuzz test `store_fuzz_test.go` to Load+Save round trips.
- **Theme spec:**
  - table tests for valid and invalid colors;
  - glyph rejection (`"\x1b[31m"`, `"‮"`, width 3);
  - `extends` chains and cycles;
  - 64 KiB cap; unknown-key warning;
  - `TestUserThemeContrastWarning` (low contrast gives a warning, not a failure);
  - `LoadDir` ignores non-`.yaml` files and symlinks pointing outside the dir.
- **EvalConfig:**
  - set, get and opt;
  - the theme is captured;
  - `rex.on`, `rex.send` and `os` are nil;
  - `dofile` and `load` are nil;
  - `require("x")` resolves only in `lua/`, and `require("../../etc")` fails;
  - `while true do end` times out within 1 s plus slack;
  - a bad id becomes a warning with the line number;
  - a runtime error is all-or-nothing;
  - `BenchmarkEvalConfig` (target under 2 ms for a 50-line config).
- **userconfig:**
  - layering order;
  - `CheckPrivate` refuses a group- or world-writable file or a foreign owner (unix build tag; it is skipped on Windows, and a test asserts it is a no-op there);
  - `Stamp` changes on an edit in `lua/`.
- **TUI:**
  - `TestConfigReloadAppliesTheme` (write file, send tick msg, run Cmd, Update; the palette changed on the Update goroutine);
  - `TestReloadKeepsPreviousOnLuaError`;
  - `TestLuaManagedRowLocked`;
  - `TestThemeCommand`;
  - `TestDynamicThemeOptions`;
  - goldens: one new settings golden with a `lua` badge, and a glyph-override golden.
- **Daemon:**
  - `TestSettingsHubSubscribers` (fires only for changed subscribed ids);
  - `TestHUPReloadsSettings`;
  - the moved S3 `summary_detail` live-apply test;
  - `TestAutoTrustUsesHub`.
- **CLI:**
  - `rex config list -json` has `source`;
  - `config set` on a Lua key exits 2 with the location;
  - `rex theme new` refuses overwrite without `-force` and writes 0600;
  - `rex complete` never evaluates Lua (a fake config.lua with a sentinel `rex.log` is not invoked).
- **Snapshot plan:** U1's goldens only regenerate in the commit that adds glyph tokens (if defaults change, they should not) and in the settings-badge commit.

## Docs/CHANGELOG
- New `docs/howto/themes.md` (built-ins, auto, YAML schema, `rex theme`) and `docs/howto/lua-config.md` (API, precedence, `require` layout, reload, timeouts, all-or-nothing).
- `docs/settings.md`: `theme` replaces `color_scheme`; sources and precedence; sparse config.yaml; reload behavior. Remove "restart the daemon" where the hub now applies changes live.
- `docs/howto/lua-hooks.md` and the Lua README: init.lua compared with config.lua.
- SECURITY.md: trust model for config.lua and theme files.
- `docs/daemon.md`, `docs/cli.md` (`rex theme`, `config edit` targets), `docs/paths.md`, `docs/STRUCTURE.md`, `docs/agents/modules/{settings,lua,tui}.md`.
- CHANGELOG:
  - Added: themes dir, `theme` setting with `auto`, config.lua, `rex theme`, live settings reload in TUI and daemon.
  - Changed: `color_scheme` renamed to `theme` (old key still read); config.yaml stores only the keys you changed.
  - Fixed: concurrent TUI and `rex config set` no longer overwrite each other.

## Conflict map vs parallel tasks
- **S3:** `boot.WatchSummarySettings` is replaced by a `SettingsHub` subscriber. If S3 lands first, step 8 deletes `summary_settings.go` and moves its test. If U2 lands first, S3 only registers a subscriber.
- **U1:** a hard prerequisite (`catalog/theme`, `rebuildStyles` on the UI goroutine, settings detail pane, `commands` table for `:theme`).
- **T1:** `board_hidden_sections` becomes Lua-settable and live-reloadable. T1's `:bg` save path must call the new dirty-key Save; this is mechanical.
- **S4, S5:** their toggles get live apply through hub subscribers instead of a restart. Note this for their authors.
- **U3:** the hidden echo tool and visible tools are untouched. Spawn pickers become one case of `dynamicOptions`.
- **P1, P2, P3:** shared edits to `cmd/rex/main.go` (`theme` verb), `core/help.go`, `completion.go` and `setup/config.go`. These are append-only. If P1's setup step writes settings, it must use the dirty-key Save. `rex setup -force` semantics are kept: it Sets only its own keys.
- **A1, T2–T5, F1:** none.

## Security notes
- **config.lua is trusted user code, like init.lua,** but it now runs in **every** rex process that loads settings. SECURITY.md gets an "Out of scope: config.lua and `lua/*.lua` are trusted user code" entry, with the mitigations listed here.
- **Ownership check (unix):** refuse config.lua, `lua/*.lua` and `themes/*.yaml` unless the file is owned by the euid and is not group- or world-writable, and the parent directory is not group- or world-writable. This mirrors OpenSSH.
  - Effect: `sudo rex …` with a preserved `HOME` (the macOS default) does not run the user's Lua as root.
  - On Windows the check is a no-op; ACLs are documented as out of scope.
  - Recommended: apply the same check to the daemon's init.lua in a separate commit (open question 5).
- **Sandbox for liveness and predictability, not security:**
  - no os, io, debug or coroutine;
  - no dofile, loadfile or load;
  - `require` is confined to `~/.config/rex/lua/`;
  - 1 s timeout through `SetContext`, bounded call stack;
  - memory is **not** capped (documented);
  - no `rex.send/on/list`, so config.lua cannot type into PTYs.
- **Theme YAML is treated as untrusted data** (people share themes). It goes through a strict regex and glyph validation that blocks terminal-escape injection ("untrusted bytes reaching your terminal" is in scope), plus the size and count caps and the symlink-outside-dir check.
- Lua-supplied strings that reach the screen (`prompt_glyph`, theme names, glyphs) go through the same validation and `protocol.FlattenDisplayText`.
- Shell completion and `-json` paths never execute Lua.
- Polling reads only the user's own config files.

## Open questions for user
1. **Architecture** (the key decision): YAML themes plus a per-process sandboxed `config.lua` (L), or YAML themes only with Lua staying hooks-only (M)? **Recommended: the hybrid.** Option (c), the daemon serving settings, is not recommended.
2. A separate `config.lua`, or `rex.set` inside the existing `init.lua`? **Recommended: separate.** init.lua's side effects and 5 s budget would otherwise run in every rex command.
3. Precedence: Lua over config.yaml, with locked rows in the TUI? **Recommended: yes**, code is the source of truth. The alternative (Lua as defaults under YAML) leaves edits silently shadowed.
4. Rename `color_scheme` to `theme` with an alias, and make config.yaml sparse? **Recommended: yes to both.**
5. Apply the owner and mode check to init.lua as well? **Recommended: yes**, as its own commit with a clear log line when it refuses.
6. Lua keybinding remap (`rex.map`) now or as a follow-up? **Recommended: follow-up (U2b),** built on U1's tables.
7. Ship `auto` as the default theme? **Recommended: no.** Keep `default` and make `auto` opt-in, because of the OSC 11 query on terminals that never answer.
8. `rex config edit` with no argument: open config.lua (new) or init.lua (today)? **Recommended: config.lua**, with `hooks` for init.lua and a one-line hint.

## Ordered steps (each one commit, builds, `make check` green)
1. `feat(paths): config.lua, lua/ and themes/ locations` (tests keep 100%).
2. `fix(settings): read-modify-write sparse Save with provenance` (dirty tracking, Source, Validate export; fixes the lost update). CHANGELOG Fixed.
3. `feat(settings): theme setting replaces color_scheme (alias)` (Aliases mechanism; TUI dynamic options; `:theme`).
4. `feat(theme): user theme files with extends and glyph tokens` (spec.go, validation, TUI glyph tokens; goldens unchanged).
5. `feat(lua): sandboxed config evaluator for settings and themes` (features/lua/config.go, example template, benchmark).
6. `feat(config): layered userconfig loader with ownership checks` (new package; STRUCTURE.md; coverage floor).
7. `feat(tui): load config.lua at boot, lock Lua-managed rows, auto theme` (splash step; detail badge; detection in `Run`; setup wizard uses the user's theme; goldens regenerated for the badge).
8. `feat(daemon): SettingsHub with polling, SIGHUP and reload intent` (migrates main.go, trust.go, lua.go; replaces or absorbs S3's watcher).
9. `feat(tui): live config reload` (tick, `configReloadedMsg`, `applyLive` diff; `:reload`).
10. `feat(cli): rex theme list/show/new; config list sources; config edit targets`.
11. `docs: themes and Lua config guides, settings, security, changelog`.
12. (Optional, per question 5) `fix(lua): refuse a group/world-writable or foreign-owned init.lua`.

## Size estimate
**L** (11–12 commits). With option (a) only it is **M**: steps 1–4 and 7–11 without Lua.

### Critical Files for Implementation
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/styles.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/settings.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/help.go
- /Users/tristan/Documents/personal/dev/rex/internal/catalog/settings/store.go
- /Users/tristan/Documents/personal/dev/rex/internal/features/lua/runtime.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/prompt.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/splash_steps.go
- /Users/tristan/Documents/personal/dev/rex/cmd/rex-daemon/main.go
