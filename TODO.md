# ToDo

- [ ] steal the best ideas from opencode and impliment them in rex (like the kanban board, the summary, the audit trail, etc) (done: R01 spike, R01-1 turn snapshots + `rex undo`/`redo`, R01-5 `rex export`, audit trail (R06 W4); left: R01-2 session diff, R01-3 prompt templates, R01-4 spawn presets)
- [ ] add qwen code and oh my pi and cursor cli and grok build to the options, have settings auto detect which models are installed and available and show them in the list of options (done: R02 qwen, cursor, grok + model auto-detect, `extra_agents`/`model_autodetect`; left: oh my pi, dropped in R02 D1)

## Workspaces / state

- [ ] per-workspace `.rex` folder (like cursor's `.cursor`) instead of JUST relying on the daemon: holds the rex config, per-folder harness history, and state (list of agents, their states, etc). Copy the folder to another machine and it works. Daemon can still sync between machines but is not required. Make a spec, plan it, then implement on a new branch. (done: R06 spec + W1 ids/colors, W2 workspace boards + `sessions.jsonl`, W3 `.rex/config.lua`, W4 audit; left: W5 system-wide management; daemon sync dropped, R06 D4)
  - [x] `rex` cmd itself does not change: it is the system-wide session (R06: `rex here` / `rex ws open` open workspace boards)
  - [ ] per-workspace sessions get a unique color on the top / bottom / left / right edges of the window, and a unique rex id (random 8-char string, generated at session creation) shown in the top left corner. The id identifies the session in the audit trail, the cli cmds and the daemon (so the daemon can sync it between machines) (done: R06 W1/W2 8-hex id + Okabe-Ito color, `[<id> <name>]` in the header, border in workspace color, `workspace_id` in daemon/audit/CLI; left: daemon sync, dropped in R06 D4)
  - [ ] system-wide rex also has a unique edge color and rex id in the top left. It can access, create, delete, rename and recolor all per-workspace sessions, and sync them between machines (done: `rex ws ls/rename/color/init` from anywhere; left: system board id + edge color, delete, W5 deferred; sync dropped)
  - [ ] design how system-wide rex manages per-workspace sessions and syncs them between machines (done: R06 spec, sync decided as none in rex, `.rex/` travels with the folder; left: W5 design, deferred)
  - [x] audit trails (save an audit log of completed tasks, filterable) (R06 W4: `audit.jsonl`, `rex audit --ws/--all/--since/--tool/--state`)
  - [ ] cli cmds for all of this (done: `rex ws init/id/ls/rename/color/trust/open`, `rex here`, `rex audit`; left: workspace delete, W5)
- [ ] multiplayer?
- [ ] rex fork for research only? instead of using google?

## Board / sessions

- [ ] add sections (can be changed in settings): ToDo, In Progress, In review, Testing, Ready for Release, Done
- [ ] linear sync 
- [ ] loop workflows (epic) (done: R12 repeat loops, `rex loop`, `loops`; left: chained loops)

*** ## Summaries / ollama

*** ## UI / settings

*** ## Setup / spawn

- [ ] (epic) i insert mode auto chooses model based onn prompt and use case 

## In progress

- R16 new `n` agent page (spec + plan, not built)
- v2.0.0 release (`release/v2.0.0`: `rex update`, install.ps1 releases, help fixes, release workflow; not merged)

## Done

- [x] make it so that it skips "yes, trust this folder" *** (PR #10: set auto_trust_dirs; setup-wizard prompt is a follow-up)
- [x] add another col that shows the # of subagents used in the past and or currently running (PR #12: currently running only)
- [x] improve overall TUI *** (PR #11: simple board)
- [x] add sound on marked as complete *** (PR #8)
- [x] cmd to delete all completed prompts *** (PR #9: rex prune / :prune)
- [x] better render engine / memory allocation manager *** (PR #7)
- [x] bug: when i go into a long-running claude agent the actual ui will start to not render and will be a bit choppy, and i have to press qq and go back into it for the issue to be solved, and even then it will go back to breaking after awhile. also this happens when the window of the claude agents stays open. *** (PR #6 + #7)
- [x] bug: sometimes when I open an agent window that is running it will just be black (PR #16)
- [x] replace google gemini to antigravity and improve functionality with it like you did with claude code (yes, trust this folder etc) *** (PR #15; hook-based state needs your OK to write agy's global hooks.json)
- [x] screensaver when idle: keeps the mac screen on, minimal unicode rex logo centered with working / waiting / completed counts (PR #19)
- [x] add a "master agent" that summarizes what is going on, what is completed, and what is ready for review (like a project manager) (S4 overview: `overview_enabled`, header line, `:overview`, `rex overview`; R03 closed as its duplicate)
- [x] make settings its own full screen page with a better layout and more options (like themes, colors, etc), as well as the help page, and make it so that the settings can be saved to a config file and loaded on startup (like cursor's .cursor) ANTI (R04: full-screen settings/help pages, `fullscreen_pages`; U1/U2 themes incl. dracula, tokyo_night, pantone_safe; config.yaml + workspace `.rex/config.lua`)
- [x] remke the settings config in lua not yaml (R05: `config.lua` layer, `lua_config`; YAML stays the TUI write target per R05 decision)
- [x] filter options for only showing certain steps of the kanban / hiding completed *** (T1: `:filter`, `:hide`/`:show`, `zh`/`z1-9`, `board_filter`)
- [x] add start / complete timestamps *** (T2: `ended_at`, span column, `rex ls -wide`, `timestamps`)
- [x] have a column for the --resume id so that in the completed section we can resume it if needed and know the id *** (T3: resume column, `:rid`, `rex resume`, `resume`)
- [x] add the cmd "uc" to undo marking an agent as "complete" (or "done") and move it back to working / needs approval *** (T4: `uc`, `:uc`, `:resume`, `undo_complete`)
- [x] search function *** (T5: `/` and `:search`, `search`)
- [x] :usage page, shows the usage of each agent, the total usage, and the usage of each model (like cursor's :usage) and remaining quota for each model (like cursor's :usage) (R10: `:usage` page, header meter, `rex usage`)
- [x] fix the ollama preview of what the agent is doing *** (S1)
- [x] change ollama to give more desc / better prompt for summarizing *** (S2)
- [x] remake the summary col to not say or start with "claude is..." instead, be more desc. adjust desc amt in settings *** (S3: opener stripped, `summary_detail`)
- [x] remake the UI and the color scheme; better help menu, remake the help and settings page *** (U1 + R04: new themes, help/settings pages)
- [x] make more settings modular and editable / hackable via lua config files and add themes *** (U2 YAML themes + `rex theme`, R05 `config.lua`)
- [x] add a cmd to add a thick colored border around the current rex window (configurable in settings) that is random by default but can be set to a specific color in settings default colors use pantone colors and are colorblind friendly. *** (U4: `:border`, `window_border`, `window_border_color`)
- [x] make cols fixed width and not resizeable by default (configurable in settings) but responsive to window size changes including a mobile version (configurable in settings) *** (U5: `column_layout`, `mobile_layout`)
- [x] fix antigravity logo in the acutal agent window (not the board) to be centered and not cut off on the right side *** (U6: vt10x REP/tabs, spawn at popup size)
- [x] change gemini to antigravity and improve functionality with it like you did with claude code (yes, trust this folder etc) *** (PR #15 + A1: OSC 7501 status, resume id, trust fallback)
