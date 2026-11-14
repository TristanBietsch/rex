# R16 New-agent flow: one full-screen page with context

Status: spec draft 2026-11-11, decisions D1–D12 open (recommended answers below). Builds on R04 (page frame), P2 (wizard trust line) and R02 (extra agents, detected models). Task plan: [R16-00-plan.md](R16-00-plan.md).

## Summary
- `n` / `:new` opens a **full-screen page**, `∴ REX › NEW AGENT`, with the R04 frame (3-row header, divider, footer hints, window border, theme tokens) instead of the 78-column popup over a dimmed board.
- Two screens instead of up to five steps: **Compose** (where you land) and **Agent** (pick agent, model, effort and skills on one screen). Defaults come from the last launch, so the common path is `n`, type, `enter`.
- A **Context** section takes extra input three ways: type it, paste it (bracketed paste, multi-line, big pastes become chips), or import workspace files and folders (`@` inline or `ctrl+o` picker). Each item can be previewed, edited and removed before launch.
- Context reaches the agent through the prompt argv (never a shell): pasted text inline, inside caps; workspace files as relative paths the agent reads itself. Nothing is copied out of the workspace and rex makes no network call.
- The P2 trust choice (`ctrl+t` / `ctrl+a`) becomes a **Trust** section of the page.
- The old popup stays behind the flag `new_agent_page` (default on). Off means today's wizard, byte-identical.

## Why
- The popup is 80 columns wide, so at 80×24 its right border clips (`…` on every row in `wizard.txtar`). It also hides the board behind it, and it does not match the R04 pages.
- Up to five screens (provider, model, effort, skills, describe) for a choice that is usually "same as last time".
- Cursor CLI reports ~200 models; the model step can only be scrolled with `j`/`k`, with no filter.
- The task is one line. Pasting a stack trace or a spec is a common reason to start an agent, but a pasted newline is just a character today, and there is no way to point the agent at files.
- On a failed launch the wizard is already closed and the typed task is gone.

## Goals / non-goals / anti-goals
- Goals: matches R04 (page, chrome, tokens); works at 80×24 and under 60 columns; keyboard-first; at most two screens; good defaults; context by typing, pasting and importing; safe delivery to every agent CLI; old flow behind a flag.
- Non-goals: prompt templates (R01-3) and spawn presets (R01-4), which plug in later as a Compose row; auto model (R13); mouse click targets (wheel only, as R04 D6); fetching URLs.
- Anti-goals: shell strings; copying workspace files into rex state; reading a file outside the session folder without the user picking it; a paste that launches the agent; color as the only signal.

## Today's flow (integration/todo `af38ab7`)

| # | Screen (popup title) | Shown when | Keys | Field |
|---|---|---|---|---|
| 0 | open | `n`, `:new` (`keys.go` `runNew`, `update.go`) | | `openWizard`: `pickerTools` (installed agents only; `extra_agents` gates qwen/cursor/grok; missing ones go to Settings → Onboarding). "no agent is installed" error when none. Notice when `default_spawn_tool` is not offered. |
| 1 | `which provider?` | always | `j`/`k`, `enter`, `esc` | `ToolIdx`, list of 4–7 tools with brand glyph + name |
| 2 | `which model?` | tool has >1 model | `j`/`k`, `enter`, `b`, `esc` | `ModelIdx`; detected models first, tagged `detected`; starts on `default_spawn_model` for the quick-spawn tool |
| 3 | `reasoning effort?` | model has an effort block | same | `EffortIdx`, starts on the model default |
| 4 | `load skills? (optional)` | `spawn_skills` on, library not empty, tool has `skill_args` | `j`/`k`, `space`, `enter`, `b`, `esc` | `SkillSel` |
| — | ~~name~~ | removed (round 1 **D21**, `0bf135f`) | | names come from `rex new -name` or `r` on the board |
| 5 | `describe the task` | always | type, `backspace`, `b` (only when empty), `enter` launch, `esc` | `TaskText`, one line; the slug `<tool>.<model>.<task-kebab>` is derived |
| 5+ | P2 trust line (branch `feat/w8-p2-trust`, not merged) | describe step | `ctrl+t` trust this session, `ctrl+a` always trust (saves the repo folder to `auto_trust_dirs`, D8) | `Trust wizTrustMode`; shows `folder not trusted: the agent will ask` and the exact folder each key trusts, or `trusted (auto_trust_dirs)`, or why the folder can't be trusted |
| 6 | launch | `enter` | | wizard closes, `NewSession{ToolID, ModelID, Effort, Slug, Title, CWD: spawnCWD, InitialPrompt, Skills, TrustFolder, Cols, Rows}`; an error shows on the board and the text is gone |

The daemon delivers `InitialPrompt` as one argv element (`prompt_args`, with the leading-dash guard) or pastes it into the PTY (`prompt_paste`, ollama). There is no size cap today; a huge prompt fails at `exec` (`E2BIG`).

## Design

### Shape
Two screens on one page, plus two pickers that open inside the right pane:

| Screen | What | Reached by |
|---|---|---|
| **Compose** | agent summary row, task editor, context list, trust line, launch | `n` lands here |
| **Agent** | agents left, models / effort / skills right | `ctrl+g` or `enter` on the agent row in Compose; `esc` back |
| Import picker | fuzzy workspace file/folder finder | `ctrl+o`, or `@` in the task editor (inline variant) |
| Preview | one context item: content head, size, path; edit / remove | `enter` on a context row |

The left pane is a **step rail** (≥ 60 columns): `Agent`, `Task`, `Context`, `Trust`, each with a state word (`✓`, `·` empty, count, `ask`/`session`/`always`). Under 60 columns it becomes the R04 tab strip `‹ Task ›`. The rail is progress and navigation: `tab` / `shift+tab` move focus between sections, the rail cursor follows.

### Defaults
- Agent, model, effort: the last launch from this page (D3), else `default_spawn_tool` / `_model` / `_effort`, else the first offered tool. A remembered agent that is no longer offered falls back the same way, with today's `unavailableNotice`.
- Skills: none. Trust: the folder's current state (P2 rules). Context: empty.
- Draft: an unsent task and its context survive `esc` and a failed launch, in memory, and come back on the next `n` (D4). `ctrl+u` on an empty editor or `:new!` clears it.

### Mockups (80×24)

All mockups are plain text; the brand glyph colors and theme tokens are listed after each. Header: `pageHeader("NEW AGENT", middle, right, w)` from R04 (blank row, title row, divider). Footer: `pageFooter(hints, w)` (divider + one hint row). Body: 19 rows.

**A. Compose, first open (empty state)**
```
                                                                                
  ∴ REX › NEW AGENT                 ~/dev/rex                       draft        
────────────────────────────────────────────────────────────────────────────────
  STEPS          │ AGENT                                                         
  ✓ Agent        │ ◆ Claude Code  ·  opus  ·  effort max          ctrl+g change  
  ▸ Task         │                                                               
  · Context      │ TASK                                                          
  ? Trust   ask  │ ┌───────────────────────────────────────────────────────────┐ 
                 │ │ █                                                         │ 
                 │ │ describe the task. @ adds a file, paste adds context      │ 
                 │ │                                                           │ 
                 │ └───────────────────────────────────────────────────────────┘ 
                 │ CONTEXT                                                       
                 │   none yet: type @path, paste, or ctrl+o to import            
                 │                                                               
                 │ TRUST                                                         
                 │   folder not trusted: the agent will ask                      
                 │   ctrl+t this session only · ctrl+a always (~/dev/rex)        
                 │                                                               
                 │                                                               
────────────────────────────────────────────────────────────────────────────────
  enter launch · ctrl+j newline · @ file · ctrl+o import · tab next · esc close
```
Tokens: `styleLogo`; rail `styleSectionTitle`, cursor `styleArrow` + `styleSelected`; placeholder `styleMuted`; editor border `colorBorderFocus` when focused, `colorBorder` otherwise; `?` and `ask` `styleBootWarn` (P2's style); `✓` `colorSuccess`.

**B. Compose, filled (task + three context items)**
```
                                                                                
  ∴ REX › NEW AGENT                 ~/dev/rex                       draft        
────────────────────────────────────────────────────────────────────────────────
  STEPS          │ AGENT                                                         
  ✓ Agent        │ ⬢ Grok Build  ·  grok-build-0.1  ·  effort medium             
  ✓ Task         │ TASK                                                          
  ▸ Context  3   │ ┌───────────────────────────────────────────────────────────┐ 
  ✓ Trust  sess  │ │ the auth test is flaky on CI, see the paste. fix it and   │ 
                 │ │ add a regression test.                                    │ 
                 │ └───────────────────────────────────────────────────────────┘ 
                 │ CONTEXT                                  3 items · 7.4 KB    
                 │ ▸ file   internal/auth/auth_test.go        4.2 KB             
                 │   dir    internal/auth/testdata/           12 files           
                 │   paste  paste 1  "--- FAIL: TestLogin (0.…"  38 lines 2.1 KB  
                 │                                                               
                 │ TRUST                                                         
                 │ ▸ ctrl+t this session only                                    
                 │     /Users/dev/dev/rex                                        
                 │                                                               
                 │ prompt 2.9 KB of 64 KB · slug gk.grok-build-0-1.the-auth-test 
────────────────────────────────────────────────────────────────────────────────
  enter preview · x remove · ctrl+o import · tab next · ctrl+s launch · esc close
```
Kind words (`file`, `dir`, `paste`, `url`) are text, not color only. The last body row is the launch summary: prompt bytes against the cap and the derived slug. While focus is in the context list `enter` previews, so `ctrl+s` launches from any section; in the task editor plain `enter` launches too (D2).

**C. Agent screen**
```
                                                                                
  ∴ REX › NEW AGENT › AGENT         ~/dev/rex                                   
────────────────────────────────────────────────────────────────────────────────
  AGENTS         │ MODELS  Cursor CLI                  / filter   200 · 3 shown 
    ◆ Claude Code│ > son█                                                        
    ◇ Codex      │ ▸ sonnet-4.6               detected                           
    ◈ Antigravity│   sonnet-4.6-thinking      detected                           
    ◉ Ollama     │   claude-4.5-sonnet        detected                           
    ◇ Qwen Code  │                                                               
  ▸ ▶ Cursor CLI │                                                               
    ⬢ Grok Build │ EFFORT  (none for this model)                                 
                 │                                                               
  not installed  │ SKILLS  optional                                              
    · DeepSeek   │ [x] go-testing    table-driven tests and goldens              
                 │ [ ] release       cut a release branch                        
                 │                                                               
                 │                                                               
                 │                                                               
                 │                                                               
                 │                                                               
────────────────────────────────────────────────────────────────────────────────
  j/k agent · tab models · / filter · h/l effort · space skill · enter done
```
- The agent list uses each tool's brand glyph and color (`brandStyle`), always next to the name. Not-installed tools show under a dim `not installed` header with `·` and are not selectable; `enter` on that header opens Settings → Onboarding (D7).
- Effort is a horizontal choice `low  medium  [high]  max`: the current one is in brackets and bold, not only colored.
- `/` filters models by substring (case-insensitive) and keeps the cursor on the same id when it still matches. With no filter, `▸` starts on the remembered model.
- Narrow (< 60 cols): one column; the tab strip reads `‹ Agents · Models · Skills ›`.

**D. Import picker (right pane, from `ctrl+o`)**
```
  STEPS          │ IMPORT  ~/dev/rex                         1,204 files indexed 
  ✓ Agent        │ > auth_te█                                                    
  ✓ Task         │ ▸ [ ] internal/auth/auth_test.go                  4.2 KB      
  ▸ Context  1   │   [ ] internal/auth/testdata/                     dir         
  ? Trust   ask  │   [x] cmd/rex/auth_test_helpers.go                1.1 KB      
                 │                                                               
                 │   ! .env.test                         looks like secrets: skip
                 │                                                               
                 │ space mark · enter add 1 · → open dir · ← up · esc back       
```
- Fuzzy match over workspace-relative paths, directories end in `/`. Marked rows use `[x]`. A denylisted path shows `!` and the reason in words (D9).
- Inline `@`: typing `@` in the editor opens a 6-row completion list under the cursor with the same matcher; `enter`/`tab` inserts `@path` and adds a `file`/`dir` context item; `esc` closes the list and keeps the typed `@text`.

**E. Preview (right pane, `enter` on a context row)**
```
                 │ PASTE 1                                38 lines · 2.1 KB    
                 │ ┌───────────────────────────────────────────────────────────┐ 
                 │ │ --- FAIL: TestLogin (0.31s)                               │ 
                 │ │     auth_test.go:88: token expired: got 401, want 200     │ 
                 │ │ FAIL                                                      │ 
                 │ │ FAIL  github.com/acme/app/internal/auth 1.204s            │ 
                 │ │ … 34 more lines                                           │ 
                 │ └───────────────────────────────────────────────────────────┘ 
                 │ e edit · r rename · x remove · j/k scroll · esc back           
```
- A file shows its first lines (read-only, max 64 KiB read, binary refused) plus size and modified time; a folder shows the file count and the first entries; a URL shows the URL only.
- `e` on a paste opens it in the composer editor (multi-line) or `$EDITOR` (D6); `e` on a file opens `$EDITOR` on the real file only after a confirm line.

**F. Loading, empty and error states**
```
  CONTEXT                                                                       
    ⠋ indexing workspace… 3,812 files                    (picker usable after)  

  AGENT                                                                         
    ⠋ checking which agents are installed…                                      

  ✗ no agent is installed. Settings → Onboarding shows how to install one.     

  ✗ paste 2 is 210 KB; the limit for pasted text is 64 KB in total.            
    Save it to a file in the workspace and import it (ctrl+o).                  

  ✗ ../secrets/key.pem is outside ~/dev/rex: only workspace files can be added.

  ✗ launch failed: daemon is not running (rex daemon start). Your draft is kept.
```
- Errors start with `✗` and words; warnings with `!`; spinners use the app spinner. An error stays until the next key, never closes the page, and never drops the draft.
- The agent list loads from `pickerTools`; until the daemon's tool list is known it says so instead of showing an empty list (today's "every tool counts as installed" fallback for old daemons stays).

**G. Narrow (50×20)**
```
                                                  
  ∴ REX › NEW AGENT                       draft   
──────────────────────────────────────────────────
  ‹ Agent · [Task] · Context 2 · Trust ›          
  ◆ Claude Code · opus · max                      
  ┌──────────────────────────────────────────────┐
  │ fix the flaky auth test█                     │
  │                                              │
  └──────────────────────────────────────────────┘
  2 context items · 6.1 KB  (tab to view)         
  trust: ask                                      
                                                  
──────────────────────────────────────────────────
  enter launch · tab next · esc close             
```

### Keys

| Key | Where | Action |
|---|---|---|
| `n`, `:new` | board | open the page (Compose, draft restored) |
| `tab` / `shift+tab` | page | next / previous section (rail follows) |
| `1`–`4` | page, not in the editor | jump to Agent / Task / Context / Trust (R04 style) |
| `ctrl+g` | page | Agent screen |
| `enter` | task editor | launch (D2) |
| `ctrl+s` | page | launch from any section |
| `ctrl+j`, `alt+enter` | task editor | newline |
| bracketed paste | task editor | insert at the cursor; never launches; over 8 lines or 1 KiB → a `paste N` context item with a one-line placeholder `[paste 1 · 38 lines]` in the task (D5) |
| `@` | task editor | inline file completion |
| `ctrl+o` | page | import picker |
| `ctrl+e` | task editor or a paste preview | edit in `$EDITOR` (D6) |
| `ctrl+u` | task editor | clear the line; on an empty editor clear the draft |
| `ctrl+w`, `alt+backspace` | task editor | delete word |
| `←`/`→`, `home`/`end`, `↑`/`↓` | task editor | move the cursor (rows when multi-line) |
| `j`/`k`, `enter`, `x`/`delete`, `r` | context list | move, preview, remove, rename |
| `ctrl+t`, `ctrl+a` | page (P2) | trust this session / always; toggle, as P2 |
| `/`, `h`/`l`, `space` | Agent screen | model filter, effort, skill toggle |
| `esc` | anywhere | close the innermost thing (completion → picker → preview → Agent screen → page). Leaving the page keeps the draft. |
| `?` | page, not in the editor | help page filtered to the new-agent group |

Removed from the page: `b` back (it was a letter in the task), the one-step-per-screen `enter next`.

### Context items

```go
type ContextKind int // ctxPaste, ctxFile, ctxDir, ctxURL (ctxSession: D10)
type ContextItem struct {
    Kind  ContextKind
    Name  string // "paste 1", or the workspace-relative path with "/" separators
    Text  string // paste body (sanitized); empty for refs
    Bytes int    // paste: len(Text); file: size on disk; dir: sum of listed files
    Files int    // dir only
}
```

Limits (bytes are UTF-8 bytes after sanitizing):

| Limit | Value | Why |
|---|---|---|
| one paste | 32 KiB | a screen of logs is ~5 KiB; above this a file is the better carrier |
| all paste text | 64 KiB | stays inside the 128 KiB Linux per-argv-element limit (`MAX_ARG_STRLEN`) with room for the task and the reference list, and well inside macOS `ARG_MAX` (1 MiB) |
| whole `InitialPrompt` | 96 KiB, checked by the daemon too | new daemon guard: `bad_intent prompt_too_long` instead of a failed `exec` |
| PTY-pasted prompt (`prompt_paste`, ollama) | 16 KiB | REPLs choke on huge pastes; the page says so when the chosen tool is one of these |
| context items | 32 | the list fits a screen; more is a folder |
| folder walk | 10,000 entries or 300 ms, whichever first, skipping `.git` and (D9) gitignored files | a home-sized folder never hangs the UI; the count reads `10,000+` |
| file preview read | first 64 KiB, first 200 lines | a 4 GiB log previews instantly |
| file reference | no size cap; a warning `!` above 1 MiB ("the agent may not read all of it") | the agent reads it, not rex |

Sanitizing (pasted and typed text; one function, fuzzed):
- Keep `\n` and `\t`. Turn `\r\n` and lone `\r` into `\n`.
- Drop every other C0 and C1 control, the ANSI escape sequences (`ansi.Strip`), and the bidi controls U+202A–U+202E and U+2066–U+2069 (Trojan Source). Invalid UTF-8 becomes U+FFFD.
- A paste that is more than 1 % NUL or U+FFFD is refused as binary.

Path rules (import picker and `@`):
- Resolve against the session folder (`spawnCWD`), then `filepath.EvalSymlinks`. The result must stay inside the resolved session folder; a symlink that points outside is refused with its target named.
- Regular files and directories only. FIFOs, sockets and devices are refused (never opened: `Lstat` first, then a non-blocking `Open` only for regular files).
- Names with control characters or newlines are refused (they would break the prompt list).
- Denylist (D9): `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`, `id_ed25519*`, `*.p12`, `.netrc`, `.npmrc`, `.pypirc`, `credentials*`, `*.kdbx`. A denylisted path is shown with `!` and is not added unless confirmed.
- Never followed during the folder walk: symlinked directories (counted, not entered).
- File content is read only for the preview, by the TUI process, as the user. Nothing is copied into the session or state folders; no new file is written.

### Delivery to the agent
`spawnctx.Assemble(task, items, caps)` builds the one `InitialPrompt` string, then the daemon delivers it exactly as today (one argv element with the leading-dash guard, or PTY paste for `prompt_paste` tools). No shell, no `sh -c`, no temp file.

```
<task text, with [paste N · L lines] placeholders kept as written>

Context from rex (read before starting):
- file: internal/auth/auth_test.go
- dir: internal/auth/testdata/ (12 files)
- url: https://example.com/issue/42

<pasted name="paste 1" lines="38">
--- FAIL: TestLogin (0.31s)
…
</pasted>
```
- File and folder items are listed as workspace-relative paths: every supported agent reads files in its own folder, under its own permission rules (D8 covers per-tool `@path` syntax).
- A paste body that contains `</pasted>` gets it escaped as `<\/pasted>` so it cannot close its own block.
- Order: task, references, pastes. The order of items in the list is the order in the prompt.
- `NewSession` gets one optional field, `ContextSummary []ContextRef{Kind, Name, Bytes}`, metadata only (no content), for the card detail and audit (R06 audit keeps the first prompt line only, unchanged). An older daemon ignores it.

### Launch and errors
- Launch validates first (caps, every path still inside the workspace, at least a task or a context item, or confirm an empty launch like today), then sends `NewSession` and **keeps the page open** with `⠋ starting…` until the daemon answers. Success closes the page, selects the new card (`PendingSelectSlug`) and clears the draft. A failure shows the error on the page and keeps everything (today the wizard is gone already).
- Trust: P2's `wizardTrustRequest` runs unchanged in the launch path.

### Accessibility
- Every state has a word or glyph: `▸` cursor plus `styleSelected`, `[x]`, `[bracketed]` effort, `✓`/`·`/`?` rail marks, `✗`/`!` errors and warnings, `detected`, `not installed`.
- Rail words stand in for color: `ask`, `sess`, `always`, `trusted`.
- `theme ansi` and `high-contrast` goldens for Compose (R04 already runs them for pages).
- No blinking other than the existing cursor; spinners obey `reduce_motion` if U1/U2 ships it (else the app spinner).

## Feature flag
- `new_agent_page`: bool, default **on** (pure UI), Section Features, `ApplyLive`. Help: "`n` opens the new-agent page: task, context (paste, @file, import) and trust on one full-screen page. Off: the older popup wizard."
- Off is inert: `n` opens today's wizard and its goldens (`wizard`, `wizard_models`, `wizard_all_agents`, P2's) stay byte-identical; `ctrl+o`, `ctrl+g`, `ctrl+s`, `@` do nothing new; no workspace walk runs.
- Independent of `fullscreen_pages`: the page uses R04's frame helpers either way.
- Turning it off while the page is open closes the page; the draft is kept in memory, and the old wizard starts with the task text only (context dropped with a notice).

## Test strategy
- Goldens (`golden_test.go`, plain + styled, `go test -update`, diffs read by eye): `newagent_empty`, `newagent_filled`, `newagent_agent`, `newagent_models_filter` (200 cursor models), `newagent_import`, `newagent_preview`, `newagent_errors`, `newagent_loading`, at 120×40, 80×24 and 50×20; `newagent_frame` with the window border; `newagent_ansi` with `theme ansi`.
- Flag off: the existing wizard goldens unchanged, and a test that `n` opens `FocusWizard`.
- Unit: `spawnctx` (`Assemble` order and format, each cap at limit and limit+1, `</pasted>` escaping, sanitizer table + `FuzzSanitize`, bidi stripping), path rules (inside, `..`, absolute outside, symlink out, symlink in, FIFO, device on Unix, denylist, control chars in names, Windows `C:\` and UNC forms skipped where illegal), bounded walk (10,001 files in `t.TempDir()` stops at the cap; time budget via an injected clock).
- Keys: bracketed paste (`tea.KeyMsg{Paste: true}` with `\n`) inserts and never launches; a big paste becomes a chip; `enter` launches only from the editor; `esc` ladder; draft survives `esc` and a failed launch.
- Daemon: `InitialPrompt` over 96 KiB → `bad_intent`, nothing spawned; `ContextSummary` round-trips and an old daemon ignores it.
- Off-is-inert test per the roadmap rule.

## Decisions

All D1–D12 accepted as recommended (2026-11-11, user).

| # | Question | Recommendation |
|---|---|---|
| D1 | Land on Compose with remembered agent, or on the Agent screen? | **Compose.** One screen for the common path; `ctrl+g` for the rest. |
| D2 | `enter` in the task editor: launch or newline? | **Launch** (keeps today's habit and `i`'s); `ctrl+j`/`alt+enter` newline; `ctrl+s` launches from anywhere. 2026-11-12 (user): kept under the vim-style modes of R04 "Decision: page navigation": `esc` leaves the editor instead of closing the page, `j`/`k`/`h`/`l` navigate outside it, a Launch step takes `enter`. |
| D3 | Remember the last agent/model/effort from the page, or always start from `default_spawn_*`? | **Remember the last launch** (in memory per TUI run, falling back to `default_spawn_*`); no new setting. |
| D4 | Keep an unsent draft after `esc`/failure? | **Yes, in memory only** (never written to disk: it may hold pasted secrets). |
| D5 | Big-paste threshold for turning a paste into a chip | **8 lines or 1 KiB.** Below that it is just text in the task. |
| D6 | `$EDITOR` (`ctrl+e`) support in v1 | **Yes**: temp file `0600` in `os.MkdirTemp` (`0700`), removed after; suspend the TUI with `tea.ExecProcess`. |
| D7 | Not-installed agents on the Agent screen | **Show them dimmed under `not installed`** (unselectable, `enter` → Onboarding). Changes R02's "hidden in wizard" for the page only; the old wizard keeps hiding them. |
| D8 | Per-tool `@path` mentions (claude/qwen/cursor understand `@file`) instead of a plain list | **Plain list in v1** (works for every tool, no per-tool registry field); add `context_ref_template` to the registry later if agents ignore the list. |
| D9 | Folder walk: respect `.gitignore`? Secret denylist: refuse or confirm? | **Respect `.gitignore` via `git ls-files -co --exclude-standard` when the folder is a repo** (bounded by the same caps), plain walk otherwise. Denylist **asks to confirm** (`!` row, `y` to add anyway). |
| D10 | Import a recent session (its summary + last 200 transcript lines) as context | **Later (R16-8, out of v1)**: needs a daemon read intent and redaction rules; transcripts may hold secrets. |
| D11 | URLs | **Reference only** (listed for the agent to fetch); rex never fetches (no egress, roadmap flag rule). |
| D12 | Textarea: hand-rolled composer or add `charmbracelet/bubbles` | **Hand-rolled `composer.go`** (multi-line, rune-safe, ~300 lines): the repo has no bubbles dependency and its inputs are all hand-rolled; bubbles' textarea would also need restyling to the theme tokens. |

## Plan
See [R16-00-plan.md](R16-00-plan.md). Order: R16-1 flag + page shell + composer → R16-2 Agent screen → R16-3 `spawnctx` (pure) + daemon cap → R16-4 paste chips + preview → R16-5 import picker + `@` → R16-6 trust section + launch states + draft → R16-7 narrow layout, accessibility goldens, docs.

Depends on: R04-1 (`page.go`: `pageHeader`, `pageFooter`, `pageNarrow`, tab strip) merged; P2 (`feat/w8-p2-trust`: `wizard_trust.go`) merged. Start after both.
