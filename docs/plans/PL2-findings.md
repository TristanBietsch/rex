# PL2 cross-cutting findings (T2/T3/T4)

Three plans follow: T2, T3 and T4, separated by `=====`. Recommended merge order: **T2 → T3 → T4**. T4's dead-session path needs T3's `Resume` intent; a T3-free fallback is described in T4.

Things I found that change scope:
1. **The board no longer has a done badge.** It was removed on purpose in 397a8a6 ("drop done chip"). `docs/tui.md:48` and `:163` and `docs/agents/data/schemas.md:37` still describe it. T2 fixes those docs. T2 also keeps the simple view free of times, since the user already took a time chip out once.
2. **`:undone` has a bug.** `command.go:92` sends `done` and `undone` to the same `toggleRetire` (`retire_view.go:35`), so `:undone` on an unmarked session **marks it done**. T4 fixes it first, with a test.
3. **`last_event_at` can't serve as the "complete" time.** `UpdateLastLine` bumps it (`store.go:487-489`), so it drifts for a session the heuristic `done_regex` marked done while its process still runs. T2 adds an explicit `ended_at`.
4. **Claude Code 2.1.286 (installed here) has the flags this needs:** `--session-id <uuid>` ("must be a valid UUID"), `-r/--resume [id]`, and `--fork-session` (only with it does a resume get a new id; by default it keeps the same one). `agy` has `--conversation <id>` but no way to set the id at spawn, so agy capture is left to A1.
5. **meta.json is `json.MarshalIndent(s.Summary())` (`persist.go:107`).** A new `SessionSummary` field is saved to disk automatically. Loading still has to copy it explicitly in `fromSummary` (`persist.go:372`).
6. **Race in in-place resume.** The old supervisor goroutine's deferred `UnregisterStop` (`spawn.go:197`) could delete a new run's stop func. Gating on `!hasStop(id)` is safe because `UnregisterStop` is the last map cleanup before `close(done)`. Check-and-reserve has to be atomic (a new `reserveStop`).
