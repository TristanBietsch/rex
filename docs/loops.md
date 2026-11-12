# Loop workflows

A **loop** runs one agent session again and again until a goal is met. rex
sends your prompt, waits for the agent to finish its turn, runs a check you
wrote, and sends the follow-up prompt if the check does not pass yet. The
daemon owns the loop, so it keeps going when the TUI is closed.

Only the **repeat** kind exists. Chains of steps (plan, build, review) are
planned, not built. There are no loop cards or keys in the TUI yet: use
`rex loop` (see [cli.md](cli.md)). A loop's session is an ordinary session
on the board, with its loop id and turn in `loop_id` and `loop_iter`.

## Turn it on

Loops are off by default. Settings → Features → *Loop workflows*, or
`rex config set loops true`. Off means: no loop runs, no loop file is read,
nothing is written under `~/.local/share/rex/loops`, and `rex loop` exits 5
with `off: enable in Settings → Features (rex config set loops true)`. The
daemon reads the setting at every request and follows changes within about
5 s. Switching it off pauses every live loop.

## The loop file

`~/.config/rex/loops/<name>.yaml`. `rex loop new <name>` writes a commented
starter (mode 0600) and opens `$EDITOR` when you run it in a terminal.

```yaml
name: fix-until-green        # must equal the file name
kind: repeat                 # the only kind
tool: claude
model: sonnet
# effort: high
# cwd: /absolute/path        # else `rex loop start -cwd`, else the current folder
prompt: "Run make check and fix the first failure. Stop when it passes."
# followup_prompt: "Run make check again and fix the next failure."   # default: prompt
until:
  check: ["make", "check"]   # argv list, run in the session folder
  check_timeout: 15m         # default 15m, at most 2h
  # marker: '<promise>DONE</promise>'
  max_iterations: 8          # default 10, 1 to 100
  max_minutes: 120           # default 120, 1 to 1440
```

Rules the validator enforces (an invalid file is never run):

- `name` is lowercase letters, digits, `.`, `_`, `-` (at most 64) and equals
  the file name. `kind: chain` is refused as not supported yet.
- `tool` and `model` are required. `cwd`, when set, is absolute without `..`.
- `until` needs a `check`, a `marker`, or both.
- `check` is a **list** of arguments, never a shell string. `check: make check`
  and `check: ["make check"]` are refused. The command runs directly, with no
  shell, in the session folder. To use a shell, write it out yourself
  (`["sh", "-c", "..."]`): that is your choice and your file.
- `marker` is a regex that must match a **whole line** of the agent's output
  (surrounding spaces ignored). A sentence that quotes the marker ("I will
  print DONE when finished") does not match. Claude Code prints its text after
  a bullet, so match the bullet too: `'(?:[●⏺] )?<promise>DONE</promise>'`. The
  marker may not match an empty line or any line of your own prompts (the
  agent echoes them).
- The caps above are enforced; unknown keys are errors.

Files are read only from `~/.config/rex/loops/`. A **symlink**, a file that
is not yours, and a **group- or world-writable** file are refused, because
the file decides what the daemon runs (`chmod 600` fixes the last one). The
daemon copies the validated definition into the loop's state, so editing the
file later does not change a loop that already started.

## What happens in a loop

1. `rex loop start <name>` spawns the session through the normal spawn path
   (so `auto_trust_dirs`, `skip_permissions` and the concurrency cap apply)
   with `prompt` as its first prompt. That is turn 1.
2. A **turn ends** when a Claude Code session reports a finished turn (the
   `Stop` hook, `wait_reason` `review`), or, for tools that report no reason,
   when the session goes from working to needs_input.
3. The daemon looks for a whole-line marker in the session's latest output,
   then runs `check` in the session folder (own process group, killed with
   its children at `check_timeout`, output captured to the loop log, at most
   64 KiB per run). **Check exits 0, or the marker matches: the loop is
   `done`.** A check that times out counts as not passed; one that cannot
   start (command not found) ends the loop as `failed`.
4. Otherwise, if `max_iterations` turns have run, the loop is `capped` and
   says so (`notice`). Else `followup_prompt` is typed into the session and
   the turn counter goes up. `max_minutes` of wall time also ends the loop as
   `capped`. A capped, stopped or done loop leaves its session running.

States: `running`, `paused`, `done`, `capped`, `stopped`, `failed`.

## It never answers for you

- **A permission or a question pauses the loop.** If the session needs input
  for any reason other than a finished turn (an approval, a question, or a
  last line ending in `?`), the loop pauses (`needs_input`), the session
  shows in Needs you, and rex sends nothing. When you answer and the agent
  finishes its turn, the loop carries on by itself.
- **Your keystrokes win.** Any input a client sends to the session
  (typing in an attached terminal, `rex reply`, `rex send`; not the automatic
  redraw nudge) pauses re-sends (`user_input`) until the agent has worked and
  gone idle again. If you type a draft and never send it, the loop waits;
  `rex loop resume` ends the wait.

## Limits and restarts

- At most **2** loops are live (running or paused) at once; a third
  `rex loop start` fails with `too many loops`. Starting a loop in a folder
  another live loop uses gives a notice, since they may edit the same files.
- Loop state is `~/.local/share/rex/loops/<id>.json` (0600) and the log
  `<id>.log`. Finished loops are deleted after 7 days.
- **A daemon restart leaves every loop paused**, never resumed. `rex loop
  resume <id>` respawns the session in place when its tool can resume (the
  same rule as `rex resume`), treats it as idle and carries on.
- A turn shorter than the daemon's state poll may be missed by Claude's
  hook-based detection; the loop then waits until the next turn. `max_minutes`
  still ends it.

See also: [cli.md](cli.md#rex-loop), [protocol.md](protocol.md), SECURITY.md.
