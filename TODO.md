# ToDo

## Workspaces / state

- [ ] per-workspace `.rex` folder (like cursor's `.cursor`) instead of JUST relying on the daemon: holds the rex config, per-folder harness history, and state (list of agents, their states, etc). Copy the folder to another machine and it works. Daemon can still sync between machines but is not required. Make a spec, plan it, then implement on a new branch.
  - [ ] `rex` cmd itself does not change: it is the system-wide session
  - [ ] per-workspace sessions get a unique color on the top / bottom / left / right edges of the window, and a unique rex id (random 8-char string, generated at session creation) shown in the top left corner. The id identifies the session in the audit trail, the cli cmds and the daemon (so the daemon can sync it between machines)
  - [ ] system-wide rex also has a unique edge color and rex id in the top left. It can access, create, delete, rename and recolor all per-workspace sessions, and sync them between machines
  - [ ] design how system-wide rex manages per-workspace sessions and syncs them between machines
  - [ ] audit trails (save an audit log of completed tasks, filterable)
  - [ ] cli cmds for all of this
- [ ] multiplayer?
- [ ] rex fork for research only? instead of using google?

## Board / sessions

- [ ] add sections (can be changed in settings): ToDo, In Progress, In review, Testing, Ready for Release, Done
- [ ] filter options for only showing certain steps of the kanban / hiding completed
- [ ] add start / complete timestamps
- [ ] have a column for the --resume id so that in the completed section we can resume it if needed and know the id
- [ ] add the cmd "uc" to undo marking an agent as "complete" (or "done") and move it back to working / needs approval
- [ ] search function
- [ ] usage meter top right (config in settings)
- [ ] Jira / linear sync ?
- [ ] loop workflows (epic)

## Summaries / ollama

- [ ] fix the ollama preview of what the agent is doing
- [ ] change ollama to give more desc / better prompt for summarizing
- [ ] remake the summary col to not say or start with "claude is..." instead, be more desc. adjust desc amt in settings
- [ ] master agent option (feature toggle) that summarizes what's going on / completed / ready for review
- [ ] add banner above claude code that is a marquee summary of what we are doing, where we are, etc..

## UI / settings

- [ ] remake the UI and the color scheme; better help menu, remake the help and settings page
- [ ] make more settings modular and editable / hackable via lua config files and add themes
- [ ] rm all other agents in the settings

## Setup / spawn

- [ ] add a step to setup: add name, choose model, and select any skills to inject into it
- [ ] improve workflow and rm the need for: /ob, then "yes, i trust"
- [ ] harden the cli

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
