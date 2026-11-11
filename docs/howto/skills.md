# How to load skills into one session

Prerequisite: [quickstart.md](../quickstart.md). Needs Claude Code; Antigravity has no per-session plugin flag, so it cannot load skills this way.

## Goal

Give one agent session extra skills (instructions Claude Code follows) without touching `~/.claude`.

## Steps

1. Put each skill in its own folder under `~/.config/rex/skills/`, with a `SKILL.md`:

```text
~/.config/rex/skills/
  review/
    SKILL.md
    checklist.md        # optional extra files the skill refers to
  release-notes/
    SKILL.md
```

```markdown
---
name: review
description: Review a diff for bugs, risky changes and missing tests.
---

Read the diff. List problems by severity. Say what you would test.
```

   The folder name is the skill's name: lowercase letters, digits and hyphens, up to 64 characters. `description` is what rex shows in lists.

2. Check rex sees it:

```sh
rex skills
```

3. Load skills into a new session, from the wizard or the CLI:

   - TUI: press `n`. After provider, model, effort and name comes a **skills** step: `j`/`k` to move, `space` to toggle, `enter` to continue.
   - CLI: `rex new -tool claude -model opus -skill review -skill release-notes "tidy the changelog"`.

4. In Claude Code, the skills appear as `rex:<name>` in `/skills`.

## How it works

When you start the session, the daemon copies the chosen skills into `~/.local/share/rex/sessions/<id>/plugin/` and starts Claude Code with `--plugin-dir` pointing at it. The copy is a snapshot: later edits to the library do not change a running session, and the copy is removed with the session. A resumed session loads the same folder again (and copies it from the library if it is gone).

Rex never writes to `~/.claude`. It writes its own `plugin.json`, and copies only regular files, at most 256 files and 4 MiB per session. A symlink inside a skill, or a skill folder that is itself a symlink, is refused.

## Switching it off

Settings → Features → `spawn_skills` (or `rex config set spawn_skills false`) hides the wizard step, makes `rex new -skill` and `rex skills` exit 5 with `off: enable in Settings → Features (rex config set spawn_skills true)`, and adds no `--plugin-dir`.

## Another tool

A tool loads skills per session when its registry entry has `skill_args` with the `{plugin_dir}` placeholder, for example `skill_args: ["--plugin-dir", "{plugin_dir}"]` (the built-in `claude` entry). See [registry.md](../registry.md).

## Troubleshooting

- `skill "x" not found in the library`: the folder has no `SKILL.md`, or the name differs. Run `rex skills`.
- `tool antigravity cannot load skills per session`: pick a tool whose registry entry has `skill_args`.
- `this rex-daemon ignored skills; run rex daemon restart`: the running daemon is older than this feature.
- A skill is missing from `rex skills`: it is a symlink, its name has uppercase letters or underscores, or it has no `SKILL.md` (a warning says which).

## See also

- [cli.md](../cli.md) (`rex new`, `rex skills`), [tui.md](../tui.md) (wizard), [settings.md](../settings.md), [protocol.md](../protocol.md) (`NewSession`).
