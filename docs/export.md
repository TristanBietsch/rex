# Session export

`rex export` writes one session as a readable document: its metadata plus the transcript as clean text. It is local only: output goes to stdout or a file, nothing is uploaded and there are no share links.

```sh
rex export <sel> [-o json|yaml|md|txt] [--out FILE] [--force] [--max-frames N] [--from-agent-log]
```

In the TUI, `:export [sel] [json|yaml|md|txt]` writes the session to `~/.local/share/rex/exports/<slug>-<shortid>.<ext>` (mode 0600, replacing an earlier export of the same session) and shows the path. The export runs in the background, so the board stays responsive: the status line says `exporting <name>…` and then `exported <path> (private, may hold secrets)`. A second `:export` of the same session while one runs is ignored.

Both belong to the `session_export` feature (Settings → Features, on by default). Off, `rex export` exits 5 with `off: enable in Settings → Features (rex config set session_export true)` before it contacts the daemon, and `:export` shows the same message.

## Privacy

An export holds whatever the agent printed: file contents, command output, possibly secrets. Files are written with mode 0600, nothing sends them anywhere, and the TUI notice says so. The same rule as for the repository applies: never paste live session output into a PR, an issue or a commit; use a made-up example.

## Formats

| `-o` | What you get |
|------|--------------|
| `json` (default) | `{"version":1,"session":{…},"source":"frames","frames":[{"at":N,"text":"…"}]}` |
| `yaml` | The same document as YAML (keys in the same order) |
| `md` | A heading, a metadata table, then `## Frame N` (or `## Turn N · role`) sections with fenced text |
| `txt` | `key: value` metadata lines, then each frame or turn after a rule |

Without `-o`, the format comes from the `--out` extension (`.json`, `.yaml`, `.yml`, `.md`, `.markdown`, `.txt`); an unknown extension, or stdout, means `json`. `-o` wins over the extension.

`session` is the same object `rex ls -o json` prints (tool, model, effort, name, slug, cwd, start and end times, state, resume id, description). It never contains environment variables. `source` is `frames` or `agent_log`. A document has `frames` or `turns`, not both. `notice` says why the export is not what you asked for. `frames_dropped` counts older frames cut by `--max-frames`. The document is versioned (`version: 1`); a breaking change to its shape bumps it.

## How the transcript becomes text

The transcript is the raw PTY byte stream, so it is never exported as is.

- **Full-screen tools** (Claude Code, Codex, Antigravity, any tool you added to `tools.yaml`) are replayed through the terminal emulator (the one attach uses) on a 120 x 32 screen. A frame is the screen text, plus the lines that scrolled off it, at each cut point: before every clear-screen (`CSI 2J`, `3J`), before every alternate-screen switch (`CSI ? 47`, `1047` or `1049` `h`/`l`), and at the end. Blank frames and frames equal to the one before are dropped, so a redraw-heavy session collapses to its distinct screens. `at` is the byte offset in `transcript.log` where the frame was cut: the transcript has no timestamps.
- **Line tools** (`echo`, `ollama`) are stripped of escape sequences and give one frame. A carriage return overwrites the line so far, so a progress bar keeps only its last state.
- Invalid UTF-8 becomes U+FFFD. Control characters are dropped from the text, including the C1 range (`U+0080`-`U+009F`, which a terminal reads as escape introducers) and DEL, and so are bidi overrides (`U+202A`-`U+202E`, `U+2066`-`U+2069`); only line breaks and tabs stay. Session metadata strings (name, description, folder, and so on) get the same treatment. Operating system commands (clipboard writes, window titles, hyperlinks, rex's private ones) are never run and, in the 7-bit form (`ESC ]`), leave no text. The 8-bit forms are not sequences to the emulator: their introducer is dropped but any payload that followed stays as plain text, harmless because the control characters are gone.
- Line tools also honour erase-in-line (`CSI K`) and cursor-column (`CSI G`), so a repaint that erases the line and writes a shorter one, or a `CSI 1 G` spinner, shows the last state.

Limits: the replay keeps the 120 x 32 size, so resizes during the session are not followed. There are no turn cuts yet: the turn ledger (R01-1) does not exist, so the extractor cuts only on screen events. A tool that repaints in place without ever clearing the screen still gives frames: every 200 lines that scroll off the screen are cut into a frame of their own. Line tools are cut the same way, 200 lines per frame.

A running session is exported up to the size its transcript has when the command opens it. The file is opened read-only, so the daemon keeps writing undisturbed. A pruned or removed session has no transcript to export; use the audit trail for its metadata.

## Output file

`--out FILE` writes the document to FILE with mode 0600 and prints what it wrote. It refuses to replace an existing file (exit 6) unless you pass `--force`, which replaces it atomically (a temporary file renamed over it, so the mode is 0600) and still refuses a symbolic link. The folder must exist.

`--max-frames N` keeps the last N frames (or, with `--from-agent-log`, the last N turns) and reads with memory bounded to about N frames; `frames_dropped` or `turns_dropped` says how many older ones were left out. Output above 20 MiB is refused (exit 6) with a hint to use it. A line-tool transcript above 512 MiB, or an agent log above 200000 messages, is refused too (exit 6); read it with `rex log`.

Without `--out`, the document goes to stdout in every mode, including agent mode (`--agent`, `REX_AGENT=1`, or auto-detected under Claude Code): the document is already machine-readable, so it is not wrapped in agent lines. With `--out`, agent mode prints one `export path=… format=… source=… frames=… turns=… bytes=…` line and the `ok` trailer. A notice goes to stderr as `rex: export: …`.

The output kit's `-o text|txt|md|json|agent` before the command (`rex -o md export p1`) is refused: it would be read as the export format. Put the format after the command. `--agent` before or after the command is fine.

## Claude's own log: `--from-agent-log`

Claude Code keeps its own conversation log as JSONL under `~/.claude/projects/`. With `--from-agent-log`, `rex export` reads `~/.claude/projects/<project>/<resume id>.jsonl` (the session's resume id, see `rex ls`) read-only and exports the real conversation: `turns` of `{role, text, at}`. Only roles and message text are kept; each tool call becomes one line (`[tool] Edit /path/to/file`); thinking, tool results, meta messages and subagent messages are left out.

It is opt-in: rex never reads Claude's files otherwise. To find the file it walks `~/.claude/projects` (following a symlinked root, at most two folders deep, skipping folders it cannot read) looking for a file named `<resume id>.jsonl`; it opens only that file and never reads any other conversation.

When the file cannot be used, the export falls back to frames and says why in `notice` (and on stderr): the tool is not Claude Code, the session has no resume id (the `resume` feature is off, or the id was not captured), or the file is not there. A line of the log that is not JSON is an error, except an unfinished last line (Claude may be writing it).

## Exit codes

| Code | When |
|------|------|
| 0 | Written |
| 1 | No transcript (pruned session), read or write error |
| 2, 3 | Selector not found, or ambiguous |
| 4 | Daemon not running |
| 5 | Bad arguments, or `session_export` is off |
| 6 | `--out` file exists (no `--force`) or is a symbolic link, the output is over 20 MiB, or the input is over the 512 MiB / 200000-message limit |
