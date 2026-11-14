# R08 Spike: "rex fork for research only, instead of using google?"

## Goal
The TODO line is ambiguous. Readings:
1. A **research mode** inside rex: spawn agents whose job is to research a question (web search, read docs, write a cited answer) instead of the user googling; results land as notes.
2. A **separate fork/binary** of rex tuned for research (different defaults, no coding tools).
3. Use rex's "fork a session" idea (branch an existing agent's context into a side session to research something without polluting the main one). Several CLIs support this (`qwen --fork-session`, `omp --resume` variants, Claude `--fork-session`).

## Options
| Option | What ships | Size |
|---|---|---|
| A. Research preset | `rex research "question"`: spawns the configured research tool/model with a read-only tool set, prompt template that requires sources, writes the answer to `~/.local/share/rex/research/<slug>.md` (or `.rex/research/` with R06); the board marks it with a plain `R` | M |
| B. Fork-a-session | `f` on a card: new session that resumes the same agent conversation as a fork (T3 resume ids + per-tool fork args), tagged "fork of <name>" | M |
| C. Separate binary | `rex-research` build with its own defaults | L, duplicates A |

## Method
1. Confirm meaning with the user (decision below).
2. For A: check which installed CLIs can run with web search but no write tools (claude `--allowedTools`, agy, omp `--tools`, qwen). For B: check fork flags per tool and T3's resume block.
3. Write the build spec for the chosen option.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Meaning | **Deferred.** Ignored for round 2. |
