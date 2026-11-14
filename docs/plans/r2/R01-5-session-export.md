# R01-5 Session export

## Summary
- `rex export <sel> [-o json|yaml|md|txt] [--out FILE]` (default `json`, D3) writes one session as a readable document: metadata (tool, model, effort, name, cwd, start/end times, state, resume id, description) plus the transcript as clean text.
- Never raw PTY bytes. Local output only (stdout or a file); nothing is uploaded. Like opencode's `export`, minus share links.
- TUI: `:export [sel]` writes to the export folder and shows the path. Behind `session_export` (on).

## Current behavior
- `rex log <sel>` (`internal/surface/cli/inspect/log.go`) streams `transcript.log`, with `--raw`, tail and follow. Output is terminal-oriented.
- Transcripts are raw PTY bytes (escape sequences, redraws). For full-screen agents (Claude, agy) the byte stream is mostly screen repaints, so "stripping ANSI" gives a messy log, not a conversation.
- Sanitizers exist: `protocol.SanitizeDisplayText`, `FlattenDisplayText` (`wire/protocol/display.go`), `pty.sanitizeForDisplay`.

## Design

### Text extraction (D1)
- Full-screen tools: **replay through the terminal emulator.** Feed the transcript into a headless vt10x screen (the same one attach uses) at the session's size, and emit the screen's text at cut points. The transcript has no timestamps, so "the screen settled" cannot be measured; cut points are each clear-screen or alt-screen switch, each turn boundary from the R01-1 ledger when snapshots are on, and the end. Consecutive duplicate frames are dropped. Result: a sequence of readable "frames".
- Line-oriented tools (echo, ollama): plain ANSI strip is enough; chosen per tool by the registry's attach mode.
- Optional source for Claude: the agent's own session log (Claude writes JSONL under `~/.claude/projects/`) keyed by T3's resume id gives the real conversation. Read-only, opt-in (D2), never written.

### Formats
- `json` (default): see below.
- `yaml`: the same document as `json`, as YAML (`gopkg.in/yaml.v3`, already a dependency).
- `md`: front matter table of metadata, then `## Turn N` sections (or `## Frame N`) with fenced text blocks.
- `json`: `{version: 1, session: SessionSummary, frames: [{at, text}] | turns: [{role, text, at}]}` (P4 versioning rule).
- `txt`: frames separated by a rule.
- Size caps: 20 MiB output; above that `--max-frames` hint.
- Default output: stdout; `--out` path (format inferred from the extension when `-o` is absent); TUI writes to `~/.local/share/rex/exports/<slug>-<shortid>.json`, 0600, and `:export md` picks another format.

### Privacy
- Exports contain whatever the agent printed (possibly secrets). Files are 0600; the TUI notice reminds the user. Never auto-attached anywhere. The repo's "no live session output in commits/PRs" rule is repeated in `docs/export.md`.

## Feature flag
- `session_export` (on). Off = `rex export` and `:export` answer "off".
- Off-is-inert test in R01-5-1.

## Plan
1. **R01-5-1 Frame extractor.** `internal/features/export`: `Frames(r io.Reader, cols, rows int, cuts []time.Time) ([]Frame, error)` using vt10x; `Strip(r io.Reader) (string, error)` for line tools. Tests: synthetic transcripts (hand-written escape sequences, never real captures): redraw-heavy input collapses to distinct frames; alt-screen enter/exit; CR-overwrite progress bars collapse; invalid UTF-8 replaced.
2. **R01-5-2 Formatters.** json/yaml/md/txt writers. Tests: golden files from a synthetic session; json and yaml carry `version: 1` and decode to the same struct; extension inference; metadata redacts nothing but includes no environment variables.
3. **R01-5-3 CLI `rex export`.** Selector, `-o`, `--out` (refuses to overwrite without `--force`), P4 agent mode. Tests: 0600 file mode; stdout default.
4. **R01-5-4 Claude log source (if D2 = a/c).** Reads only `~/.claude/projects/**/<resume id>.jsonl`, roles + text only, tool calls summarized as one line. Tests with a synthetic JSONL in a fake HOME; missing file → falls back to frames with a notice.
5. **R01-5-5 TUI `:export` + docs.** Command-table row; `docs/export.md`, `docs/cli.md`, CHANGELOG.

## Review focus
- Transcript of a running session: export reads up to the current end; no lock contention with the PTY writer (open read-only, stat size first).
- Pruned session: nothing to export; message suggests R06 audit for metadata.
- Terminal resized during the session: the emulator follows the recorded resizes if the transcript has them; otherwise frames use the last known size (documented).
- Transcript contains escape sequences that try to set the window title or write files (OSC 52 clipboard): the emulator ignores them; output contains text only.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | PTY bytes to text | vt10x replay for full-screen tools, ANSI strip for line tools. |
| D2 | Claude's own log | Opt-in `--from-agent-log`; frames by default. |
| D3 | Default format | `json`. Also `md`, `txt` and `yaml`. |

## Build notes

Built on `integration/todo` (branch `feat/r01-5-export`). Where the code differs from the plan above:

- **No registry attach mode.** The tool registry has no per-tool attach mode. `export.LineTool` names the line tools (`echo`, `ollama`); every other tool, including a user-added one, is replayed through vt10x. This mirrors the attach code's `redrawKickTools`, which is also a list in code.
- **Cut points are byte offsets, not times.** The transcript has no timestamps, so `Frames(r, cols, rows, cuts []int64)` takes transcript byte offsets and `Frame.At` is the offset. The R01-1 turn ledger does not exist yet: `Options.Cuts` is the hook for it, and nothing sets it today.
- **Scrolled-off lines are kept.** The vt10x scrollback hook feeds each frame the lines that scrolled off the primary screen since the previous cut, so a line-by-line session does not lose its top rows.
- **Fixed replay size.** The transcript records no resizes and `meta.json` holds no PTY size, so the replay uses the daemon default, 120 x 32.
- **`rex export` has its own `-o`** (json, yaml, md, txt), not the output kit's. The kit is used for the `--out` report (agent line, json envelope) and for errors. Stdout carries the bare document in every mode. A kit `-o` before the command is refused.
- **`yaml` is produced from the JSON** (read as YAML, re-emitted in block style), so both always carry the same keys.
- **`--from-agent-log` for non-Claude tools or sessions without a resume id** falls back to frames with a notice, like a missing file, instead of failing.
- **The TUI `:export`** runs in the background (`exportDoneMsg`), one per session at a time. `rex export --max-frames` has no TUI counterpart.
- **Review fixes:** text and session metadata drop C1, DEL and bidi runes; `Frames` cuts every 200 scrolled lines and `FramesLimited` keeps a bounded ring; `Strip` honours `CSI K` and `CSI G`; yaml is built from the JSON token stream, not by re-parsing it; forced writes are atomic and refuse symlinks; the agent log lookup follows a symlinked root, skips unreadable folders and skips oversize lines.
