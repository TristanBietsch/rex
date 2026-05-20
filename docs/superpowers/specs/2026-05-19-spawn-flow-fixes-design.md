# Spawn-flow fixes — design

**Status:** draft
**Date:** 2026-05-19
**Scope:** make AI summaries actually populate the description column; fix the wizard's "describe the task" prompt being silently dropped by Claude / Codex / Gemini; rewire `i` to quick-spawn a configurable default agent (out of the box: Claude Code, opus, max effort).

## Problem

Three independent bugs in the session spawn flow conspire to make the experience worse than the design intends.

1. **The description column is empty for every session, so it falls back to raw last-line output.** The AI-summary pipeline is fully wired (summarizer worker at `internal/summarizer/`, `Description` field on `protocol.SessionSummary`, `DescAnim` typewriter render at `internal/tui/board.go:194`, settings keys at `internal/settings/registry.go:95-108`), and `summary_enabled` defaults to true. The daemon log for the user's current session shows the actual failure:
   ```
   INFO  summarizer: started              model=gemma2:2b base_url=http://127.0.0.1:11434
   WARN  summarizer: backend_unavailable  reason="model not pulled: gemma2:2b"
   INFO  daemon: summarizer health flip   available=false
   ```
   The user has `llama3.1` pulled — not `gemma2:2b`. The worker flips to permanently unavailable and emits zero summaries. The TUI then displays raw `LastLine` output (spinner glyphs, splash text, last typed line) instead of an AI-generated description.

2. **The wizard's "describe the task" prompt is silently dropped for Claude / Codex / Gemini.** Only Ollama actually processes the typed prompt. From the daemon log at the time of the user's screenshot, all four sessions report `pty: initial_prompt pasted` AND `pty: initial_prompt submitted` — so the supervisor believes it succeeded. The actual failure is at `internal/pty/supervisor.go:116-170`: the readiness detection ("at least one chunk of output AND 800ms of quiet") fires while Claude / Codex / Gemini are mid-render of their splash banner, **before** the input field is initialized. The bracketed-paste sequence lands against a non-existent input box and is dropped. Ollama, which prints `>>> ` and parks immediately, satisfies the 800ms gate cleanly and gets the prompt.

3. **Pressing `i` always spawns an `echo` session.** `internal/tui/update.go:411-417` hardcodes `ToolID: "echo", ModelID: "short"`. The user wants `i` to be a fast path to Claude Code with `max` effort, with the choice configurable in settings.

## Goals

- AI summaries flow without requiring the user to know what model name to pull. If the configured `summary_model` is missing, the worker substitutes the first acceptable pulled model and logs the substitution.
- The initial prompt reaches Claude, Codex, Gemini, and Ollama identically — by using an adapter-supplied readiness signal instead of a one-size-fits-all timing heuristic.
- `i` performs a one-keypress quick-spawn into a user-chosen default `(tool, model, effort)`. The CLI peer (`rex new`) and Lua hook layer stay unchanged in this work.

## Non-goals

- No protocol break. Wire format and persistence stay backward-compatible.
- No auto-pull of Ollama models without consent. We substitute if something else is pulled; we do not run `ollama pull` ourselves.
- No new entry points unified under "default spawn." `i` is the only one this spec touches. The CLI and Lua layers will be revisited in a separate spec if the inconsistency becomes a real friction.
- No richer prompt-delivery telemetry surface in this work. The 30s timeout still warns silently — same as today. If the regex never matches, the user notices and can manually retry.

## Section 1 — Summarizer model auto-discovery

### Overview

When the worker starts (or recovers from a backend-unavailable state), it enumerates Ollama's pulled models via `GET /api/tags`. If the configured `summary_model` isn't present, the worker substitutes the first match from a fixed preference list of small models, then any other pulled model. The substitution is logged once; the `summary_model` setting is not modified. Existing `SummarizerHealth` event still fires `available=false` if NOTHING usable is pulled, with a clearer `Reason` string pointing the user at `ollama pull`.

### Changes

**`internal/summarizer/client.go`** — add the missing tag-listing call:
```go
// ListPulledModels returns the names of models the local Ollama instance has pulled.
// Empty slice + nil error if the instance is reachable but has no models.
func (c *Client) ListPulledModels(ctx context.Context) ([]string, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
    if err != nil { return nil, err }
    resp, err := c.http.Do(req)
    if err != nil { return nil, fmt.Errorf("ollama tags: %w", err) }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("ollama tags: status %d", resp.StatusCode)
    }
    var body struct {
        Models []struct{ Name string `json:"name"` } `json:"models"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
        return nil, fmt.Errorf("ollama tags decode: %w", err)
    }
    out := make([]string, 0, len(body.Models))
    for _, m := range body.Models { out = append(out, m.Name) }
    return out, nil
}
```

**`internal/summarizer/worker.go`** — resolve the actual model to use on probe:
```go
// preferredFallbacks orders the small models we'd substitute in if the configured
// one isn't pulled. Anything not in this list falls back to "first pulled" in
// arbitrary order. Hardcoded for simplicity; promote to a setting later if needed.
var preferredFallbacks = []string{"gemma2:2b", "llama3.2:1b", "phi3:mini", "qwen2.5:1.5b"}

// resolveModel picks the model the worker should actually use, given a configured
// preference and what Ollama has pulled locally. Returns ("", false) if no model
// can be used (Ollama is reachable but has nothing pulled).
func resolveModel(configured string, pulled []string) (string, bool) {
    if configured == "" || !contains(pulled, configured) {
        for _, p := range preferredFallbacks {
            if contains(pulled, p) { return p, true }
        }
        if len(pulled) > 0 { return pulled[0], true }
        return "", false
    }
    return configured, true
}
```

The worker's existing `probe()` (whatever its current shape) calls `ListPulledModels` first, then `resolveModel`. If the result differs from the configured model, log:
```
slog.Info("summarizer: model_substituted", "from", cfg.Model, "to", resolved, "reason", "configured model not pulled")
```
Substituted model is stored on the worker for the lifetime of the daemon process. The user's `summary_model` setting is not touched — restarting after pulling the configured model picks it back up naturally on next probe.

**`SummarizerHealth` event reason** — extend the existing `available=false` reason from `"model not pulled: <X>"` to `"no compatible summary model pulled; try \`ollama pull gemma2:2b\`"` when the preference list is fully unpulled AND no fallback exists.

### Tests

- `summarizer/worker_test.go`:
  - `resolveModel("gemma2:2b", ["gemma2:2b","llama3.1"]) → ("gemma2:2b", true)` — configured wins
  - `resolveModel("gemma2:2b", ["llama3.1"]) → ("llama3.1", true)` — fall through to "first pulled"
  - `resolveModel("gemma2:2b", ["phi3:mini","llama3.1"]) → ("phi3:mini", true)` — preference list wins over arbitrary first
  - `resolveModel("gemma2:2b", []) → ("", false)` — nothing pulled
  - `resolveModel("", ["llama3.1"]) → ("llama3.1", true)` — empty config = use anything
- `summarizer/client_test.go`:
  - `ListPulledModels` against a `httptest.Server` returning a canned `/api/tags` payload
  - Returns `error` on non-200; returns empty slice + nil error on empty models
- Worker integration test:
  - Stand up a fake Ollama server that returns `["llama3.1"]` for tags
  - Worker started with `Model: "gemma2:2b"` substitutes and logs; subsequent `Summarize()` calls hit `/api/generate` with `model: "llama3.1"`

## Section 2 — Prompt-delivery readiness via per-adapter signal

### Overview

The supervisor's initial-prompt-typing goroutine stops using a global 800ms-quiet heuristic. Instead it asks the adapter "is the agent's input prompt on screen right now?" via a new `Adapter.IsReadyForInput` method. Each adapter answers truthfully for its own kind of CLI:

- **HeuristicCLI** returns true when the existing `prompt_regex` matches the cleaned output window — the same signal the state machine already uses for `StateNeedsInput`, applied here for a related-but-distinct purpose.
- **ClaudeStructured** returns true after the first JSON event has been parsed — Claude Code emits its `system` init event within ~300-800ms of spawn, well before the input field flickers up.

The 30s deadline stays unchanged; same silent-warn fallback if the agent never signals ready.

### Changes

**`internal/adapter/adapter.go`** — extend the interface:
```go
type Adapter interface {
    Detect(window []byte, idle time.Duration) protocol.State
    IsReadyForInput(window []byte, idle time.Duration) bool
}
```

**`internal/adapter/heuristic.go`** — implementation reuses the same window pipeline as `Detect`:
```go
func (h *HeuristicCLI) IsReadyForInput(window []byte, _ time.Duration) bool {
    tail := window
    if len(tail) > 4096 { tail = tail[len(tail)-4096:] }
    clean := ansi.Strip(cursorPositionRe.ReplaceAllString(string(tail), "\n"))
    return h.prompt.MatchString(clean)
}
```
The `idle` parameter is ignored — the visible prompt **is** the ready signal. No idle gate needed; pasting while text is still arriving is fine because the bracketed-paste markers protect us either way.

**`internal/adapter/claude.go`** — track first-event arrival:
```go
type ClaudeStructured struct {
    mu         sync.Mutex
    last       protocol.State
    buffer     []byte
    lastSeen   time.Time
    firstEvent bool
}

// inside Detect's JSON-parsing loop, after a successful Unmarshal:
a.firstEvent = true
a.lastSeen = time.Now()
// (switch on obj["type"] stays as-is)

func (a *ClaudeStructured) IsReadyForInput(_ []byte, _ time.Duration) bool {
    a.mu.Lock()
    defer a.mu.Unlock()
    return a.firstEvent
}
```

**`internal/pty/supervisor.go`** — replace lines 116-170 (the 800ms-quiet poll) with adapter-driven detection:
```go
if s.cfg.InitialPrompt != "" {
    go func(prompt string) {
        deadline := time.NewTimer(30 * time.Second)
        defer deadline.Stop()
        poll := time.NewTicker(200 * time.Millisecond)
        defer poll.Stop()
        for {
            select {
            case <-deadline.C:
                slog.Warn("pty: initial_prompt timed out waiting for ready", "session", sess.ID)
                return
            case <-ctx.Done():
                return
            case <-poll.C:
                windowMu.Lock()
                idle := time.Since(lastChunk)
                snap := append([]byte(nil), window...)
                windowMu.Unlock()
                ready := false
                if s.cfg.Adapter != nil {
                    ready = s.cfg.Adapter.IsReadyForInput(snap, idle)
                } else {
                    // No adapter (echo tool, tests) — preserve the 800ms-quiet fallback.
                    ready = len(snap) > 0 && idle > 800*time.Millisecond
                }
                if !ready { continue }
                // ── unchanged below ────────────────────────────────────────
                paste := append([]byte("\x1b[200~"), []byte(prompt)...)
                paste = append(paste, []byte("\x1b[201~")...)
                if _, err := f.Write(paste); err != nil {
                    slog.Warn("pty: initial_prompt paste failed", "session", sess.ID, "err", err)
                    return
                }
                slog.Info("pty: initial_prompt pasted", "session", sess.ID, "bytes", len(paste))
                select {
                case <-ctx.Done(): return
                case <-time.After(120 * time.Millisecond):
                }
                if _, err := f.Write([]byte{'\r'}); err != nil {
                    slog.Warn("pty: initial_prompt submit failed", "session", sess.ID, "err", err)
                } else {
                    slog.Info("pty: initial_prompt submitted", "session", sess.ID)
                }
                return
            }
        }
    }(s.cfg.InitialPrompt)
}
```

### Tests

- `adapter/heuristic_test.go`:
  - `IsReadyForInput` returns true when prompt regex matches the cleaned window (Ollama `>>> `, Codex `^› `)
  - Returns false when no match
  - Idle parameter is ignored: same answer regardless of value
- `adapter/claude_test.go`:
  - Returns false before any JSON event observed
  - Returns true after first parsed event (any type — system, assistant, user, result)
  - First-event flag survives subsequent buffer resets
- `pty/supervisor_test.go`:
  - Stub adapter whose `IsReadyForInput` returns false for the first N polls, then true; verify the paste landed only after the flip
  - Stub whose `IsReadyForInput` never returns true; verify 30s deadline fires and `pty: initial_prompt timed out` is logged (use a 100ms deadline override for the test)
  - Nil-adapter path preserves the 800ms-quiet behavior for existing echo tests

## Section 3 — Quick-spawn defaults + settings

### Overview

`i` keeps its current shape — it focuses the bottom `λ` prompt; the user types a task description; enter spawns a session. The only change is that the session is no longer hardcoded to `echo / short`. It uses three new settings keys whose out-of-the-box values are `claude / opus / max`. The user can change any of them via the existing Settings overlay (`S` key). `n` continues to open the full wizard for ad-hoc selection.

### Changes

**`internal/settings/registry.go`** — new section + three keys:
```go
const SectionSpawn Section = "Spawn"

{
    ID: "default_spawn_tool", Label: "Quick-spawn tool", Section: SectionSpawn,
    Type: TypeString, Default: "claude",
    Help: "Tool ID used when pressing 'i' in the TUI. Must match an entry in tools.yaml.",
},
{
    ID: "default_spawn_model", Label: "Quick-spawn model", Section: SectionSpawn,
    Type: TypeString, Default: "opus",
    Help: "Model ID used when pressing 'i'. Must be a model offered by the selected tool.",
},
{
    ID: "default_spawn_effort", Label: "Quick-spawn effort", Section: SectionSpawn,
    Type: TypeString, Default: "max",
    Help: "Effort tier used when pressing 'i'. Ignored for tools without an effort axis.",
},
```
Type is `TypeString` (not `TypeEnum`) because the registry is user-extensible via `~/.config/rex/tools.yaml` — we cannot pin valid values at compile time. Validation happens at spawn time.

**`internal/tui/slug.go`** — new file extracting the slug helper the wizard already uses:
```go
package tui

import "strings"

// slugWithToolModel formats <toolShort>.<modelShort>.<taskKebab>.
// Matches the scheme used by the new-agent wizard so a row's slug is
// readable as "where did it come from." Takes tool/model IDs directly —
// no registry dependency, so it works even if the IDs reference a tool
// that isn't loaded yet (the daemon will error on the spawn either way).
func slugWithToolModel(toolID, modelID, task string) string {
    return strings.Join([]string{
        toolShortID(toolID),
        modelShortID(modelID),
        task,
    }, ".")
}

// toolShortID maps known IDs to two-letter prefixes, falls back to the first
// two letters of the ID otherwise. Pull the exact mapping from wizard.go's
// existing slug logic during implementation.
func toolShortID(id string) string {
    switch id {
    case "claude": return "cc"
    case "codex":  return "cx"
    case "gemini": return "gm"
    case "ollama": return "ol"
    default:
        if len(id) >= 2 { return id[:2] }
        return id
    }
}

// modelShortID strips well-known version suffixes used by the wizard's slugs
// (e.g., "gpt-5-codex" stays as-is; "llama3.1" becomes "llama3-1"). Pull the
// exact transformation from wizard.go during implementation.
func modelShortID(id string) string {
    return strings.ReplaceAll(id, ".", "-")
}
```
The wizard switches its inline slug formatting to call `slugWithToolModel`. No behavior change; one shared source of truth. **Verify by spot-check during implementation:** if `wizard.go` does anything more elaborate (e.g., longer aliases for specific models), preserve that behavior in `modelShortID` rather than this lazy `.→-` replace.

**`internal/tui/update.go`** — rewire `spawnSessionCmd` (lines 404-422):
```go
func spawnSessionCmd(c *client.Client, store *settings.Store, prompt string) tea.Cmd {
    return func() tea.Msg {
        toolID, _  := store.Get("default_spawn_tool").(string)
        modelID, _ := store.Get("default_spawn_model").(string)
        effort, _  := store.Get("default_spawn_effort").(string)

        task := deriveSlugFromPrompt(prompt)
        if task == "" { task = "session" }
        slug := slugWithToolModel(toolID, modelID, task)

        cwd, _ := os.Getwd()
        if err := c.NewSession(protocol.NewSession{
            ToolID: toolID, ModelID: modelID, Effort: effort,
            Slug: slug, CWD: cwd, InitialPrompt: prompt,
        }); err != nil {
            return DaemonErrMsg{Err: err}
        }
        return nil
    }
}
```

**No client-side registry validation.** The daemon already validates at `handleNewSession` via `srv.Registry().FindModel(toolID, modelID)` (returns `"tool X/Y not in registry"` if unknown), which surfaces back to the TUI as the existing `DaemonErrMsg` error display. Adding client-side validation would duplicate logic and require threading the registry into `Model` — not worth it. The error message is slightly less polished ("tool claude/typo not in registry" vs. "default_spawn_model %q not available; press S to fix") but accurate and unambiguous.

Caller (`updatePromptKey` near line 388) updates the call to pass `m.Store` alongside the existing `m.Client`. No registry plumbing needed.

### Tests

- `settings/registry_test.go`:
  - Three new keys exist with documented defaults
- `tui/slug_test.go`:
  - `slugWithToolModel(claudeTool, "opus", "fix-auth")` → `"cc.opus.fix-auth"`
  - Wizard's existing output matches the helper for the same inputs (regression guard during extraction)
- `tui/update_test.go` (or new):
  - `spawnSessionCmd` with `default_spawn_tool=claude, model=opus, effort=max` → `NewSession{ToolID:"claude", ModelID:"opus", Effort:"max"}` and slug prefix `cc.opus.`
  - Invalid tool / model surfaces via the existing `DaemonErrMsg` path from daemon-side validation — covered by `server/server_test.go` rather than re-tested here

## Edge cases

- **No Ollama running.** Summarizer's existing `available=false` path fires; the new substitution code never runs. TUI shows the existing "AI summary backend unavailable" banner.
- **Ollama returns 0 pulled models.** `resolveModel` returns `("", false)`; worker stays unavailable with the clearer reason string.
- **Adapter is nil (echo tool, tests).** Supervisor falls back to the existing 800ms-quiet behavior. No regression for the echo smoke recipe.
- **User edits `default_spawn_*` to a removed model.** Next `i` press fails with a clear named-key error. Settings page is one keystroke away.
- **User presses `i` before the daemon's handshake completes.** `m.Registry` (or whatever holds the registry) is empty; same error surface as "unknown tool." Acceptable since the rest of the TUI is also non-functional pre-handshake.
- **Claude takes >30s to emit its first JSON event** (e.g., upstream auth failure). Existing `pty: initial_prompt timed out waiting for ready` warning fires. User sees no prompt in the agent and can manually re-send via `rex reply` or by attaching. Same outcome as today.
- **Prompt regex briefly matches mid-render then disappears** (e.g., Codex flashes `›` during animation). The paste happens once at first match; if the agent isn't actually ready, the bracketed-paste markers are still buffered correctly by every modern terminal — the input will land when the agent's input field initializes. This is no worse than today.

## Sequencing

Suggested execution order:

1. **Summarizer auto-discovery** — `client.ListPulledModels`, `worker.resolveModel`, health-event reason update. Independent.
2. **Adapter `IsReadyForInput` method** — interface change + heuristic + claude implementations. Independent.
3. **Supervisor readiness rewire** — depends on (2).
4. **Settings keys + slug helper** — `SectionSpawn`, three keys, `slug.go` extraction. Independent.
5. **`spawnSessionCmd` rewire** — depends on (4) and on `m.Registry` plumbing.
6. **Docs** — update `docs/settings.md` (new section), `docs/tui.md` (i now spawns Claude by default), `docs/registry.md` (note that `prompt_regex` is now also used for readiness detection).

Each numbered group lands as its own commit. Tests live with the code they cover and land in the same commit.

## Risks

- **Substituted summary model is too large for the user's machine.** If the user only has `qwen2.5:32b` pulled, we'll substitute to that and summarization will be slow. Mitigation: preference list explicitly orders small models first; we only fall through to "any pulled" if the entire preference list is missing. If real usage shows this biting, add a size cap (e.g., skip anything matching `:7b` or larger).
- **Prompt regex false positives during splash render.** Codex briefly draws `›` mid-banner; if our cleaned window catches that, we paste too early. Mitigation: the bracketed-paste markers protect against partial-text drop in most terminal implementations, and the paste happens once. If observed in practice, add a "match must persist for 2 consecutive polls" debounce — Approach B from the brainstorm.
- **`default_spawn_*` keys point at a tool the user removed from their tools.yaml.** Spawn fails with a named-key error; user opens Settings and picks a valid value. Considered acceptable — silent fallback to echo would be confusing.
- **Slug helper extraction regresses the wizard.** Mitigation: `tui/slug_test.go` asserts the helper's output matches the wizard's prior output for a fixed set of inputs — extraction is mechanical with a regression-guard test.
