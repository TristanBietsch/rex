# U2L: R05 (`config.lua`) checked against the code at 1a6b1bc

R05 is still the spec, and its decisions D1–D4 and the `lua_config` flag stand. This document lists where R05 no longer matches the code, the edits R05 needs, revised steps, and the questions R05 leaves open. All paths are relative to the repo root. Lines are cited at 1a6b1bc.

## 1. Where R05's assumptions no longer hold

| # | What R05 assumes | What the code does now |
|---|---|---|
| A1 | R05-3: "`Store.Load` runs Eval after YAML", with the evaluator in `catalog/settings/luacfg` | This would be an import cycle. `luacfg` has to import `settings` to call `settings.Validate` (`store.go:221`), and `settings` already imports `theme` (`registry.go:6`). So `settings` cannot call `luacfg`. `Load(path)` is YAML only (`store.go:68-100`). |
| A2 | One place loads settings | There are about 17 `settings.LoadDefault()` callers. Several run on **every** CLI command: `core/agentauto.go:9` (inside `InitOutput`, before dispatch, `cmd/rex/main.go:20`), `core/styles.go:143`, `core/errlog.go:160`, `core/feature.go:9`. Three more sites bypass `LoadDefault` and use `NewStore`+`Load`: the hub (`settings_hub.go:109-110`), the TUI boot step (`splash_steps.go:95-109`) and TUI live reload (`config_reload.go:84-88`). |
| A3 | `Result{Locked []string, Source string}`, and a lock shown with `file:line` | `Store` already tracks a source per key, but only `SourceDefault` and `SourceFile` (`store.go:26-34`). It has no `SourceLua` and no per-key location. `WithDefaults` copies `src` (`store.go:287-302`), so any new location map must be copied there too. |
| A4 | "U2 test already reserves" exit 5 for a locked key | No such test exists (grep finds no "managed" or `SourceLua`). The free part: `configSet` already maps any `Set` error to `ExitInvalidArgs`=5 (`setup/config.go:176-178`, `core/version.go:61`). |
| A5 | Sparse Save | It has landed (`store.go:112-196`): only dirty ids are written, and `Set`/`Reset` mark them (`:245-273`). An overlay that never touches `dirty` therefore never reaches YAML, so the R05-3 "never writes Lua keys" test passes by construction. Still test it. |
| A6 | Values pass through `Setting.Normalize` | Use `settings.Validate` (`store.go:221-241`, coerce plus Normalize). Normalize exists only on a few strings: `theme` (`registry.go:30`), `prompt_glyph` (`:49`), `auto_trust_dirs` (`:287`). Other `TypeString` rows (for example `summary_model`, `default_spawn_tool`) accept any bytes. The TUI only strips `\n` before display (`settings.go:689-701`). Validate also ignores `ReadOnly` (only `Set` checks it, `store.go:250`). |
| A7 | `lua_config_path` is a dead row that U1 removes | It is still in the registry and live: `registry.go:359-362`, read by `boot/lua.go:95-106` to choose which file the daemon runs. If Lua could set it, Lua would choose which code the daemon runs. |
| A8 | `rex.set("auto_trust_dirs", { "~/dev" })` | `auto_trust_dirs` is a `TypeString` joined with `:` (`;` on Windows) (`registry.go:285-287`). `board_hidden_sections` is joined with `,`. The registry has no list type. |
| A9 | The example uses an `os_name` global | No such global exists. |
| A10 | `rex.theme("mine", { accent=…, bg=… })` (flat keys), and "missing token falls back with a warning" | U2's schema is nested: `extends` / `dark` / `colors` / `glyphs` (`theme/spec.go:128-229`). `extends` must name a built-in (`spec.go:152-159`, `docs/themes.md:49`). Missing tokens come from the base **silently**, as documented ("Everything you leave out comes from the base theme"). `Parse` takes YAML **bytes** (`spec.go:128`), so it cannot be fed a Lua table directly. |
| A11 | Glyph and escape rules | These exist: `ValidGlyph` requires exactly 1 cell with no control, bidi or escape characters (`spec.go:308-337`). Issues pass through `printable()` (`catalog.go:31-41`, `:123-129`). `protocol.FlattenDisplayText` exists (`wire/protocol/display.go:65`). The `Catalog` holds only file themes (`catalog.go:45-49`), and `rex theme list` knows two sources, `builtin` and `user` (`setup/theme.go:36-40,78`). |
| A12 | "Refuse to evaluate, same check as `init.lua`" | `init.lua` has **no** owner or mode check (`features/lua/runtime.go:167-189`, plain `DoFile`). The check that exists is the theme rule: open once, then fstat, regular file, owned by euid, not group- or world-writable, directory checked too, symlink allowed when its target passes, `O_NONBLOCK` for FIFOs, `os.Root` on Windows (`theme/catalog.go:195-216`, `private_unix.go:12-44`, `private_windows.go`, SECURITY.md:52-66). |
| A13 | Libraries: base minus `dofile`/`loadfile`/`load`; no `package` | In gopher-lua v1.1.2 the **base** library itself installs `require`, `module`, `print`, `_printregs`, `loadstring`, `getfenv`/`setfenv` (`baselib.go:26-56`). `print` and `_printregs` write to stdout, which would corrupt the TUI frame and `-o json` output. `package.path` would be seeded from `$LUA_PATH` (`loadlib.go:65`). |
| A14 | "10 MiB allocation cap via instruction-count hook" | gopher-lua has no allocation hook and no instruction hook. `SetContext` is checked between VM instructions only. Go built-ins can run unbounded: `string.rep` uses `strings.Repeat`, and pattern matching is a backtracking engine. `CallStackSize` and `RegistryMaxSize` bound only the stacks. |
| A15 | "SettingsHub watches config.lua → TUI message" (R05-4) | The hub is daemon-only and watches one file through `fileSig` (`settings_hub.go:27-30,191-216`). The TUI has its own 2 s poller and stamp (`config_reload.go:16-18,64-73`). Its error text is hard-coded to "config.yaml: …" (`:154`). |
| A16 | Errors: "keep the last good values" | The hub's existing behavior on a YAML parse error: keep the previous values, but **fail closed** on `TrustSettingIDs` (`settings_hub.go:105-119`, `trust_ids.go:10`). Short-lived CLI processes have no last-good state. |
| A17 | Notice "in the TUI header" | Every reload and theme notice already goes to the status line (`m.Err`, `config_reload.go:154`; `theme_cmd.go:58-68`) plus a WARN row in the boot splash (`themesLoadedStep`). `header.go` is being edited by a parallel lane. |
| A18 | Locked rows only on the settings page | More code writes settings: `:theme` (`theme_cmd.go:60`), the setup wizard (`setup_wizard.go:908-915` `setOrReset`, which **Resets on a Set error**; also `:1702,:1714`), `r` reset (`settings.go:174-180`), and the spawn cascade (`settings.go:529-547`, which ignores errors with `_ =`). |
| A19 | `rex --help` caps eval at 50 ms "for colors" | CLI styling already promises that a theme problem never changes command output (`core/styles.go:123-138`). P4/D11 also fix the shape of agent and json output. Using two different caps in different processes would give them different results. |
| A20 | The settings UI is stable | R04 (full-screen settings with a two-pane inspector) is planned and not merged. It will rewrite `settings_view.go` (badge `:55-62`, rows `:215-250`). |
| A21 | `rex config edit -lua` | `config edit` opens `init.lua` (`setup/config.go:236-262`, `paths.LuaInitFile` at `:242`). The paths package has no `config.lua` or `lua/` accessors (`paths.go:63,127,213`). |
| A22 | The flag is inert | D15 requires off to mean inert. The hub and TUI stamps would still stat `config.lua` unless they are gated. If `lua_config` could be set from Lua, it would read itself. |

## 2. Edits R05 needs

**Design: evaluator**
```diff
- Eval(ctx, path string, base map[string]any) (Result, error)
- Result{Values, Themes []theme.Palette, Locked []string, Source string}
+ luacfg.Eval(ctx, Opts{File, ModuleDir, Deny []string}) Result
+ Result{Values map[string]any; SetAt map[string]string /* "config.lua:12", "lua/ui.lua:3" */;
+        Themes map[string]theme.Palette; ThemeIssues []theme.Issue; Warnings []string /* ≤20 + "N more" */; Err error}
+ No `base`/rex.get: the result depends only on the files and the platform, so the
+ daemon, TUI and CLI always agree, and it can be memoised by file stamp. Deny lets
+ R06 W3 refuse trust ids in .rex/config.lua.
- libs: base (minus dofile, loadfile, load), table, string, math
+ libs: base, table, string, math, then nil out: dofile loadfile load loadstring print
+ _printregs require module getfenv setfenv. Install our own `require(name)`: name must
+ match ^[a-z0-9_]+(\.[a-z0-9_]+)*$, maps to <ModuleDir>/a/b.lua, file read through the
+ same private-file opener, compiled with L.Load, cached, cycle detected, ≤64 modules.
+ CallStackSize 200, RegistryMaxSize bounded.
- 250 ms wall clock via context
+ 250 ms in every process (no 50 ms CLI variant, see A19). Eval runs the LState on its own
+ goroutine and returns at the deadline even when Lua is stuck in a Go built-in; that
+ goroutine is abandoned and stops at its next instruction check.
- 10 MiB allocation cap via instruction-count hook
+ string.rep capped to 1 MiB output; find/match/gmatch/gsub refuse subjects > 256 KiB;
+ script ≤256 KiB, scripts plus modules ≤1 MiB; values ≤4 KiB; heap watchdog per Q3.
- example: rex.set("auto_trust_dirs", { "~/dev" }); if os_name == "darwin" ...; rex.theme("mine", {accent=..., bg=...})
+ rex.platform (= runtime.GOOS). New registry field Setting.ListSep (auto_trust_dirs:
+ os.PathListSeparator; board_hidden_sections: ","): a Lua array of strings is accepted
+ only for those settings and joined. rex.theme uses the YAML schema
+ {extends=, dark=, colors={…}, glyphs={…}}.
+ Each rex.set: refuse an unknown id, a ReadOnly id (lua_config_path), lua_config itself,
+ and Deny ids. A Lua number for TypeInt must be integral. Every string is refused if it
+ has a control, ESC, bidi or zero-width rune, then settings.Validate runs. Every refusal
+ is a warning carrying L.Where(1); evaluation continues. A Lua error or timeout discards
+ the whole result (all or nothing).
```

**Design: themes**
```diff
- theme.Palette via U2's YAML validator; missing token → default with warning
+ theme.ParseMap(name, map[string]any) split out of Parse (Parse = decode + ParseMap),
+ so YAML and Lua share one validator: ValidName, not a built-in, extends built-in only,
+ ValidGlyph (1 cell), parseColor, printable() on issues. Missing tokens come from the
+ base silently. Catalog.WithLua(themes, issues) returns a copy; a Lua theme beats a
+ file theme with the same name and adds a warning Issue (Q6). rex theme list Source
+ gains "lua". Lua tables → map: depth ≤3, ≤64 keys per table.
```

**Design: store layer** (replaces "Store.Load runs Eval")
```diff
+ settings: SourceLua; Store.setAt map; Overlay(values, setAt); SetAt(id) string;
+ ErrManagedByLua{ID, At} ("theme is set in config.lua:12; edit it there: rex config edit -lua").
+ Set and Reset return ErrManagedByLua for a Lua-sourced id; WithDefaults copies setAt.
+ settings.SetLayer(fn) (atomic pointer; nil = YAML only) and
+ settings.LoadLayered(path) (*Store, LayerReport, error) = NewStore + Load(yaml) + layer
+ when FeatureOn(yaml-only store, "lua_config"). LoadDefault calls LoadLayered.
+ The layer is installed once in cmd/rex main() before InitOutput, skipped for the
+ `complete` and `completion` verbs, and once in cmd/rex-daemon main().
+ Tests without the layer stay YAML-only.
+ luacfg keeps a per-process memo keyed by the stamp of config.lua plus lua/**/*.lua
+ (≤256 entries, depth ≤4): one eval per process, with in-memory last-good.
+ On a Lua error: last-good overlay (or none at first start, Q1), except TrustSettingIDs,
+ which read their defaults until the file evaluates again (mirrors settings_hub.go:116).
```

**Design: reload, UI, CLI**
```diff
- R05-4 SettingsHub watches config.lua (+ TUI message)
+ Daemon: the hub's fileSig becomes a composite stamp (config.yaml, plus config.lua and
+ lua/ while lua_config is on), and reloadLocked calls LoadLayered.
+ TUI: configStamp and loadConfig (config_reload.go:67,84) and stepSettingsLoad
+ (splash_steps.go:95) call LoadLayered/the composed stamp; the message carries Lua
+ themes; a new boot row "config.lua" (OK/WARN/SKIP) after settings.load.
+ The error text changes from "config.yaml:" to the failing layer, flattened with
+ FlattenDisplayText and capped at 200 runes.
- one notice in the TUI header and in rex doctor
+ status line + splash WARN row + `rex doctor` check "config.lua" (no header.go change).
- R05-5 lock glyph
+ No emoji. Locked rows show a dim value plus a muted "lua"; the detail pane line shows
+ "set in config.lua:12" where it shows "read-only" today.
+ One guard, settingLocked(m, id), runs before enter, ←/→, r and inline edit; it puts
+ D3's message on the status line. settingAdjustable returns false for locked rows.
+ The spawn cascade refuses a tool change when a dependent key is locked.
+ `e` on a locked row runs $EDITOR via tea.ExecProcess (pattern in update.go:555);
+ +N is passed for vi/vim/nvim/nano/emacs/micro.
+ :theme refuses with the same message. The setup wizard skips locked keys and lists
+ them in its summary (setOrReset must not Reset them).
- rex config get prints source
+ The txt layout stays byte-identical (P4). config list/get gain source and set_at in
+ json/agent and a "lua" marker in the human text view.
+ `rex config set/reset` on a locked key: exit 5 with the location.
+ `rex config edit -lua` (template example_config.lua, 0600 in a 0700 dir);
+ `rex config lua-export` prints to stdout (Lua %q-safe quoting) and never writes a file.
+ Both exit 5 with the standard off message while lua_config is off.
- refuse to evaluate (same check as init.lua)
+ Factor the theme opener into internal/runtime/privfile (Open(dir, name, max), CheckDir;
+ unix and windows builds); theme uses it with no behavior change. config.lua, ~/.config/rex
+ and lua/** go through it: a symlink to a safe target is allowed, anything else is
+ refused with the reason. init.lua per Q5.
+ lua_config flag: Features, ApplyLive, read from the YAML layer only. Off: no stat or
+ read of config.lua or lua/, no eval, rows unlocked, -lua/lua-export/`e` refuse.
```

**Tests to add on top of R05's list.** All are written before the code.
- **Sandbox:**
  - `os`, `io`, `print`, `_printregs`, `dofile`, `loadfile`, `load`, `loadstring`, `module` and `package` are nil.
  - `require("../x")`, `require("a/b")` and `require("/etc/x")` are refused.
  - `require` ignores `$LUA_PATH`.
  - The stdout of an eval that tries to print stays empty.
- **CPU:**
  - `while true do end` returns in under 300 ms.
  - Deep non-tail recursion hits the stack limit.
  - A pathological pattern returns by deadline + 50 ms (abandon path).
- **Memory:**
  - `string.rep("x", 1e9)` errors.
  - A `s = s..s` loop and a 1e7-entry table stop with a memory or time error, not an OOM (Q3).
- **Escapes:**
  - `rex.set("summary_model", "\27]52;c;…")` is refused.
  - A glyph `"\27[31m"` and a width-2 glyph are refused.
  - `error("\27[2J")` is flattened in the TUI status line, `doctor` output and the daemon log.
  - Warnings are capped.
- **Ownership:**
  - Modes 0666/0620 are refused.
  - A foreign uid is refused (euid seam, `private_unix.go:36`).
  - A symlink to a safe target loads; a link to an unsafe target or a dangling link is refused.
  - A FIFO does not block.
  - On Windows, `os.Root` confinement applies to `lua/`.
- **Precedence and trust:**
  - Precedence table: default < yaml < lua.
  - `TestLuaOverlayNeverPersisted`.
  - `TestLuaErrorKeepsLastGoodButFailsTrustClosed`: Lua sets `auto_trust_dirs=""` over YAML `"~/dev"`; after the file breaks, the hub's `Current` is `""`, not the YAML value.
  - The daemon and the TUI produce an equal `Snapshot()` from the same files.
- **Flag and verbs:**
  - Off-is-inert: a stat counter shows 0 stats, no eval happens, and rows are unlocked.
  - `rex complete` never evaluates (a sentinel counter).
  - `lua_config_path` and `lua_config` cannot be set from Lua.
- **Fuzz:** `FuzzEval` (R05), plus a `ParseMap` fuzz shared with YAML.

## 3. Revised steps

Each step is one commit, builds, and passes `make check`. Docs and the CHANGELOG go in the step that changes the behavior.

1. **R05-0 `refactor(theme)`**: `runtime/privfile` opener and `theme.ParseMap`. No behavior change, and the theme package keeps its 100% coverage floor.
2. **R05-1 `feat(luacfg)`**:
   - `paths.LuaConfigFile` and `LuaModuleDir`, keeping 100% coverage;
   - the sandbox, the custom `require`, the limits, `rex.set` with `ListSep`, `rex.platform`;
   - the memo, the fuzz test and the benchmark (under 2 ms for 50 lines);
   - a new coverage floor.
3. **R05-2 `feat(luacfg)`**: `rex.theme` through `ParseMap`, and `Catalog.WithLua`.
4. **R05-3 `feat(settings)`**:
   - `SourceLua`, `SetAt`, `ErrManagedByLua` and the `Set`/`Reset` refusal;
   - `SetLayer` and `LoadLayered`, with trust fail-closed;
   - the `lua_config` flag and its off-is-inert test;
   - the layer installed in both mains;
   - `config set/reset` exits 5, with a test.
5. **R05-4a `feat(daemon)`**: the hub's composite stamp and `LoadLayered`; SIGHUP and `rex reload` work through the existing paths (`reload.go:32`).
6. **R05-4b `feat(tui)`**: `LoadLayered` in boot and reload, the "config.lua" boot row, the status-line notice, and Lua themes in the catalog.
7. **R05-5 `feat(tui)`**:
   - the lock guard and badge;
   - D3's message and the `e` editor key;
   - `:theme` refusal;
   - the setup wizard skips locked keys;
   - goldens.
   Rebase onto R04 if R04 lands first.
8. **R05-6 `feat(cli)`**:
   - `config list/get` source;
   - `config edit -lua` with its template;
   - `config lua-export`;
   - the `rex theme list` "lua" source;
   - the doctor check.
9. **R05-7 `docs`**:
   - new `docs/howto/lua-config.md`;
   - updates to `settings.md` (Files table, line 23 `config edit`, precedence), `themes.md`, `paths.md`, the Lua README and `howto/lua-hooks.md` (`config.lua` compared with `init.lua`), `agents/modules/lua.md`;
   - SECURITY.md: an out-of-scope entry ("`config.lua` and `lua/` are trusted user code; the sandbox is for liveness, not security"), and an in-scope entry ("bytes from `config.lua` reaching the terminal unflattened, or a `config.lua` owned by another user being evaluated").
10. **(Q5) `fix(lua)`**: the same owner and mode check on `init.lua`, with a daemon log line when it refuses.

**Size: L.** 9–10 commits, about 3k lines with tests. Lane C does steps 1–4 and 8; lane B does 5; lane A does 6–7.

**Conflict map**
- `header.go`, `wizard.go`, `keys.go` and board rows: no edits. The notice moved to the status line. Settings keys live in `settings.go:146-181`, and hints in `:639`.
- Light edits:
  - `splash_steps.go:241-256` (one boot row);
  - `setup_wizard.go:188,908-915`;
  - `theme_cmd.go:58-68`;
  - `cmd/rex/main.go:20` and `cmd/rex-daemon/main.go:140` (one line each).
- R04 rewrites `settings_view.go`. Keep the lock rendering in a single helper (`settingLockBadge`) so R05-5 ports easily.
- R06 W3 later calls `luacfg.Eval` with `Deny = TrustSettingIDs + lua_config_path + lua_config` behind `rex ws trust`.

## 4. Questions R05's decisions do not answer

1. **Starting a process with a broken `config.lua` (no last-good state in memory yet).**
   - **(Recommended)** Use YAML plus defaults, with trust settings failing closed, plus a notice. Simple and predictable, but a restart loses the values Lua was setting.
   - Keep a last-good snapshot on disk (`~/.local/state/rex`, 0600) and reuse it. Survives restarts, but adds a file write per good evaluation, and that file can go stale.
   - Refuse to start the TUI or daemon until the file is fixed. Impossible to miss, but contradicts "a bad config never breaks boot".
2. **What `config.lua` can read.**
   - **(Recommended)** `rex.platform` only. Every process computes identical values.
   - Add an allowlisted `rex.getenv` (`TERM_PROGRAM`, `COLORTERM`). Allows per-terminal themes, but the daemon and the TUI may disagree on values.
   - Full `rex.getenv` plus `rex.process`. Maximum flexibility, but different processes can end up with different trust settings.
3. **Memory bound**, since the instruction hook R05 relied on does not exist.
   - **(Recommended)** A heap-growth watchdog (about 64 MiB, approximate) plus the caps above. Catches runaway scripts, but it is process-wide, so the daemon could see a rare false positive.
   - Caps only, with memory left uncapped and documented, as for `init.lua`. No false positives, but `s = s..s` can still balloon.
   - Evaluate in a child process with an OS memory limit. Exact bound, but costs 5–15 ms per command and does not work on Windows.
4. **Showing a `config.lua` error in ordinary CLI commands.**
   - **(Recommended)** Say nothing in command output. Report it in `rex doctor`, `rex config …`, `rex theme list` and the TUI. Output shape stays stable (P4), but a plain `rex ls` hides the problem.
   - One stderr line per command, text format only. Visible, but noisy until the file is fixed.
   - Always on stderr, agent and json modes included. Impossible to miss, but breaks agent mode's one-line error contract.
5. **Owner and mode check on the daemon's `init.lua`** (U2 Q5, never built).
   - **(Recommended)** Yes, the same rule, as its own commit with a log line when it refuses. One trust rule for all user Lua, but an existing loose-mode file stops loading.
   - Log a warning but still run the file. No breakage, but the gap stays open.
   - Leave `init.lua` as it is. No work, but the two Lua files follow different rules.
6. **A Lua theme and a theme file with the same name.**
   - **(Recommended)** Lua wins and a warning is shown. Matches settings precedence, but the file is silently shadowed except for the warning.
   - The file wins. Shared data files take priority, but this inverts the Lua-over-YAML rule.
   - Refuse both until one is renamed. Explicit, but one typo removes a working theme.

### Critical Files for Implementation
- internal/catalog/settings/store.go
- internal/daemon/boot/settings_hub.go
- internal/surface/tui/config_reload.go (plus internal/surface/tui/settings.go)
- internal/catalog/theme/spec.go (plus internal/catalog/theme/catalog.go and private_unix.go)
- internal/surface/cli/setup/config.go
