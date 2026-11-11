# S3 Desc column: no "Claude is…", adjustable detail, readable at narrow widths

## Summary
A deterministic guard in `cleanResponse` (exported as `Clean`) strips agent-subject openers ("Claude is", "The agent is", "Agent is", "It is", "It's", "Claude Code has been", "Currently,") and sentence-cases the rest.
A new `summary_detail` setting (short/medium/long → 60/110/180 runes plus prompt wording) goes through the settings registry and reaches the daemon live, with no restart.
The board truncates descriptions on word boundaries and adds a dim preview line under the board with the selected session's full summary and its age.

## Current behavior (file:line)
- `prompt.go:43-74` `cleanResponse` strips quotes and labels and clamps to `maxDescription = 60` (:10, :70) mid-word. It has no opener handling, so "Claude is currently working on the" uses up the 60 chars.
- Settings: `catalog/settings/registry.go:121-138` has `summary_enabled`, `summary_model`, `desc_animation`. The daemon reads them once at startup (`cmd/rex-daemon/main.go:117-123`; `docs/daemon.md:45`: restart needed).
- Board:
  - `board.go:495-504` `renderRow` → `cell` → `truncate` (:686) truncates by columns with "…" mid-word;
  - the detailed-view description gets the leftover width (`layoutRows` :400);
  - the simple view shows `rowText` (title);
  - `anim.go:26` `renderAnimFrame` cuts by runes without an ellipsis.

## Design
**Guard (`prompt.go` → `Clean(s string, maxRunes int, tool string) string`; `cleanResponse` stays as a test shim).** The order matters for safety:
1. `protocol.SanitizeDisplayText(s, 0)`.
2. Strip labels. The existing list gains `Status line:` and `Summary:`.
3. Strip leading `- `, `> `, `* `, then quotes.
4. **Strip the opener.** The regex runs on sanitized text only:
   ```
   (?i)^(?:(?:right\s+)?now,?\s+|currently,?\s+|at\s+the\s+moment,?\s+)?
     (?:the\s+|this\s+)?(?:(?:ai|coding|cli)\s+)?
     (?P<subj>agent|assistant|model|bot|session|claude(?:\s+code)?|codex|antigravity(?:\s+cli)?|agy|gemini(?:\s+cli)?|ollama|grok|<quoted tool id>|it|he|she|they|we|i)
     (?:\s+(?:is|are|am|was|has\s+been|have\s+been|will\s+be|seems\s+to\s+be|appears\s+to\s+be)|['’](?:re|m))
     \s+(?:(?:currently|now|still|actively|busy|in\s+the\s+process\s+of)\s+)*
   ```
   - The `'s` form is handled separately: `^(subj)['’]s\s+` is stripped only when the next word ends in "ing" ("Claude's editing x" → "Editing x"; "Claude's config updated" is kept). For `it|he|she` it is always stripped ("It's compiling").
   - Then a lone adverb opener (`^(currently|right now|now|at the moment),?\s+`) is stripped.
   - Repeat at most twice ("Currently, Claude is…").
5. Drop the trailing period.
6. Sentence case: upper-case the first rune only when the first word is all lowercase letters ("running" → "Running"; "go.mod updated" and "pnpm…" are left alone).
7. If fewer than 3 letters remain, return "". The worker then keeps the previous description (existing behavior at worker.go:230).
8. Clamp to `maxRunes` on a word boundary: cut to `maxRunes-1`, back up to the last space if it falls within the final 40%, trim trailing `,;:—-`, append "…".

**Setting (`registry.go`, Section `SectionSummary`, inserted right after `summary_model`):**
```go
{ID: "summary_detail", Label: "Summary detail", Section: SectionSummary,
 Type: TypeEnum, Default: "medium", Options: []string{"short", "medium", "long"},
 Help: "How much the AI description says: short ≈ 8 words, medium ≈ 15, long ≈ 28. Applies within seconds."},
```
- `summarizer.ParseDetail(string) Detail` maps unknown values to medium. `Config.Detail` is set in `main.go`.
- `Worker.SetDetail(Detail)` stores it in an atomic. The hash dedupe changes on its own, because the length rule is part of the prompt.
- Live apply: new `internal/daemon/boot/summary_settings.go` `WatchSummaryDetail(ctx, path string, w *summarizer.Worker, every time.Duration)` stats `config.yaml` every 5s; when the mtime changes it reloads with `settings.NewStore().Load(path)` and calls `w.SetDetail`. It is wired in `main.go` next to `ProbeOllamaHealth`.

**Board.**
- (a) `renderRow`'s description cell uses a new `truncateWords(s, n)`: sanitize, word-boundary cut, "…". Other cells keep `truncate`.
- (b) `renderAnimFrame` animates `truncateWords(to, width)`, so the last frame equals the settled cell.
- (c) New `desc_preview.go`:
  - `previewRows(m Model) int`: 0 when h < 20 or no session on the board has a description; 2 when h ≥ 30 and detail is long, else 1. It is reserved for the whole board, so the height doesn't jump as the selection moves.
  - `renderDescPreview(m, cw)`: dim `  ↳ <full FlattenDisplayText(Description)> · 12s`, with the age from S1's `DescriptionAt` via `durationAgoAt`. Word-wrapped (`ansi.Wordwrap`) to `previewRows` lines, the last one truncated with "…".
  - Shown when the selected row has a Description that its row doesn't fully show (always in the simple view, since that shows the task; in the detailed view only when truncated). Otherwise the reserved line stays blank.
  - Rendered between the board and the bottom HR in `renderFullScreen`.
  - `keymap.go`: const `boardBottomRows = 3` becomes `func boardBottomRows(m Model) int { return 3 + previewRows(m) }`, used by `boardHeightFor`.
  - Mouse hit-testing is unaffected: `sessionAtScreenRow` already ignores rows past the board.
- `rowText` and `rowDescription` keep their names and behavior (T5's search haystack depends on them).

## Files to change
- Summarizer: `prompt.go` (`Clean`, guard, clamp), `prompt_test.go`, `detail.go` (`ParseDetail`), `worker.go` (`SetDetail`, `Config.Detail`), `config.go`.
- Settings: `catalog/settings/registry.go` (`Registry` entry), `registry_test.go`.
- Daemon: `internal/daemon/boot/summary_settings.go` (+ test); `cmd/rex-daemon/main.go` summarizer block.
- TUI:
  - `surface/tui/board.go`: `renderRow` description cell, new `truncateWords`;
  - `anim.go`: `renderAnimFrame`;
  - new `desc_preview.go`;
  - `keymap.go`: `boardBottomRows`, `boardHeightFor`;
  - `tui.go`: `renderFullScreen`;
  - snapshot tests.
- `wire/protocol/display.go`: comment only.

## Test plan (test-first)
- `TestClean_StripsSubjectOpeners`, table-driven:

| Input | Expected |
|---|---|
| `Claude is running the tests` | `Running the tests` |
| `The agent is currently editing main.go` | `Editing main.go` |
| `Agent is waiting for approval` | `Waiting for approval` |
| `It is compiling` | `Compiling` |
| `It's compiling` | `Compiling` |
| `Claude Code has been refactoring auth` | `Refactoring auth` |
| `The AI assistant is now reviewing PR #12` | `Reviewing PR #12` |
| `Currently, Claude is fixing lint` | `Fixing lint` |
| `Codex is running go test ./...` | `Running go test ./...` |
| `I'm reading docs` | `Reading docs` |
| `Claude's editing x.go` | `Editing x.go` |
| `\x1b[1mClaude is\x1b[0m running` | `Running` (sanitize first) |

- Negatives that must stay unchanged:
  - `Claude's config file updated`;
  - `CLAUDE.md updated`;
  - `Itemizing invoices`;
  - `Isolating flaky test`;
  - `Agentic loop tuned`;
  - `Waiting: approve git push`;
  - `Done: tests pass`.
- `TestClean_OpenerOnlyYieldsEmpty`: `Claude is` → "".
- `TestClean_SentenceCase` (including `go.mod updated` unchanged); update the existing lowercase expectations.
- `TestClean_ClampsOnWordBoundary` per detail.
- `FuzzClean` with random `maxRunes ∈ {60,110,180}`: terminal-safe, ≤ max runes, and idempotent (`Clean(Clean(x)) == Clean(x)`).
- Worker: `TestWorker_SetDetailChangesPromptAndCap` (fake server captures `num_predict` and System).
- Boot: `TestWatchSummaryDetail_AppliesOnMtimeChange` (temp `HOME`/path, write the file, expect `SetDetail`).
- Settings: `TestRegistry_SummaryDetail` (enum, default medium, invalid value rejected by the store).
- TUI:
  - `TestTruncateWords`;
  - `TestRenderAnimFrame_EndsAtSettledText`;
  - `TestPreview_ShownForSelectedSummary_SimpleView`;
  - `TestPreview_HiddenWhenFitsDetailed`;
  - `TestBoardHeight_StableAcrossSelection`;
  - `TestMouse_PreviewRowsHitNothing`;
  - inline snapshot at 80 and 140 columns.
- `make check`, and raise the coverage floor if it rose.

## Docs/CHANGELOG
- `docs/settings.md` AI summary table: `summary_detail`, its mapping, and "applies within ~5s".
- `docs/daemon.md:45`: exception for `summary_detail`.
- `docs/tui.md`:
  - ROW COLUMNS: word-boundary truncation and the preview line;
  - layout line accounting.
- CHANGELOG:
  - Added: `summary_detail`, preview line.
  - Changed: descriptions never start with the agent's name, sentence case, longer by default (medium).

## Conflict map
- `registry.go`:
  - S3 inserts after `summary_model`.
  - T1 appends `board_hidden_sections` (append-only).
  - S4/S5 should add theirs after `desc_animation`.
  - U2 (Lua settings) reshapes it last.
  - All merges are trivial.
- `board.go`: S3 changes only the one description-cell line in `renderRow` and adds `truncateWords`. T2/T3/T4 change `rowCols`, `layoutRows` and the `renderRow` column writes nearby, so expect a small hunk conflict; land S3 after T2–T4 or rebase. `rowText` and `rowDescription` are unchanged (T5 safe).
- `tui.go` `renderFullScreen` and `keymap.go` `boardBottomRows`: T5's search bar and S5's banner also add rows. Agree that each feature contributes through a `bottomRows`/`headerHeight` function, not constants.
- U1 (UI remake, last) restyles the preview line.
- `cmd/rex-daemon/main.go`: S1 (wiring) and S4 (fleet) touch the same block.
- `boot/`: new file only.

## Security notes
- The guard runs after `SanitizeDisplayText`, and the clamp uses runes. Output stays one line, valid UTF-8 and ≤200 (the protocol cap). Fuzzed.
- The opener regex is anchored and linear (RE2), with no catastrophic backtracking.
- The preview line renders through `FlattenDisplayText` like every cell.
- The settings watcher only reads `config.yaml`, which is already trusted. Unknown enum values fall back to medium.

## Open questions for user
1. Default detail: medium (110) or long? Recommended: medium. On this machine each step up adds roughly 0.5–1.5s per call.
2. Reserve a preview line under the board, or skip it and rely on the detailed view only? Recommended: the preview line (one row, two on tall terminals with long detail).
3. Live apply of `summary_detail` via a 5s file poll, or "restart the daemon" like the other summary settings? Recommended: live.

## Ordered steps
1. `feat(summarizer): strip agent-subject openers and sentence-case descriptions`: `Clean` and tests, at 60 runes.
2. `feat(summarizer): word-boundary clamp and Detail caps`: `ParseDetail`, `SetDetail`, `num_predict` per detail.
3. `feat(settings): summary_detail` (registry plus `main.go` startup read).
4. `feat(daemon): apply summary_detail live from config.yaml`: `boot.WatchSummaryDetail`.
5. `feat(tui): word-boundary description cells and settled animation`: `truncateWords`, `renderAnimFrame`.
6. `feat(tui): selected-session summary preview line`: `desc_preview.go`, `boardBottomRows(m)`, `renderFullScreen`.
7. `docs: summary_detail, preview line` plus CHANGELOG.

S2 step 3's `TestExamplesObeyRules` lands after S3 step 1.

## Size estimate
M
