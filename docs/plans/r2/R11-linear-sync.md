# R11 Linear two-way sync

## Summary
- Decided (D1): **Linear only, two-way.** Jira is out.
- In: Linear issues (filtered by team / label / assignee) become R09 **draft** cards in ToDo, prompt = issue title + description, card linked to the issue key.
- Out: card stage changes update the Linear issue's workflow state, and finishing a card posts one comment (S1/S2 description + duration + resume id). No other writes.
- Starts with a short API spike (R11-0), then build. Off by default behind `linear_sync` (Settings → Features).

## Depends on
- R09 drafts (D3) and stages (hybrid). R06 audit for the "who/what changed" trail. P4 output kit for `rex linear` CLI.

## Design

### Auth and secrets
- Personal API key or OAuth app token, stored in the OS keychain (macOS Keychain, libsecret, Windows Credential Manager) via `rex linear login`; env `REX_LINEAR_TOKEN` as override. Never in `config.yaml`, Lua, `.rex/`, logs or transcripts.
- Daemon makes no network calls while `linear_sync` is off.

### Mapping
- Setting `linear_filter` (team key, label, assignee=me), `linear_poll` (default 2 min).
- Stage ↔ workflow state map, per team, editable:
  ```yaml
  linear_state_map:          # rex stage id -> Linear state name
    todo: "Todo"
    doing: "In Progress"
    review: "In Review"
    testing: "In Review"
    release: "Ready for Release"
    done: "Done"
  ```
- Unmapped stage → no write. Linear state with no mapping → card stays where it is.

### Sync rules
- Pull: new matching issues → drafts (never auto-spawned). Issue moved to a Linear "completed"/"canceled" type state → card marked done (not deleted).
- Push: card stage change (manual or automatic) → `issueUpdate(stateId)`, debounced 30 s so flapping working ↔ needs-you does not spam. Card completion → one comment.
- Conflicts: last writer wins by timestamp; rex never overwrites a Linear change newer than its last pull (it re-pulls first and drops its push with a notice).
- Every write recorded in the audit trail (R06) with issue key and old/new state.

### Daemon
- `internal/features/linear`: GraphQL client (`https://api.linear.app/graphql`), poller, pusher. Rate-limit aware (respect `X-RateLimit-*`, back off on 429).
- `SessionSummary.IssueKey`, `IssueURL` (additive).

## Feature flag
- `linear_sync` (off). Off = no network, no poller, no keychain reads, `rex linear` commands refuse.
- Lives in Settings → Features (roadmap "Feature flags"). Its owning step adds an **off-is-inert** test: with the flag off, no goroutine/subprocess/network/file write happens and the feature's keys and commands answer "off: enable in Settings → Features".
- Flag lands in step R11-1.

## Plan
0. **R11-0 Spike (docs only).** Confirm GraphQL queries/mutations (`issues` with filter, `workflowStates`, `issueUpdate`, `commentCreate`), auth options, rate limits, webhook vs poll. Prototype a read-only pull against a test team the user provides. Update this file.
1. **R11-1 Token store.** Keychain wrapper + `rex linear login|logout|status`. Tests: env override; token never printed (`status` shows last 4 chars only).
2. **R11-2 Client.** Queries + mutations behind an interface. Tests against an `httptest` GraphQL stub: pagination, 429 backoff, auth error → feature pauses with notice.
3. **R11-3 Pull → drafts.** Tests: new issue creates one draft; re-poll is idempotent (keyed by issue id); canceled issue marks card done.
4. **R11-4 Push.** Tests: stage change → one `issueUpdate` after debounce; flapping within 30 s → one write; Linear changed newer → push dropped; completion → one comment.
5. **R11-5 UI.** Issue key badge on card, `o` opens URL, `:linear` status line. Goldens.
6. **R11-6 Docs + SECURITY.md** (network egress, token storage).

## Review focus
- Issue description with prompt-injection text becomes a draft prompt: drafts are never auto-spawned; the user reads before `enter`.
- Token revoked mid-run: pauses sync, one notice, no retry storm.
- Two machines running rex on the same Linear team: both push the same state change; idempotent (no-op if state already matches).
- Offline laptop: queued pushes expire after 24 h rather than replaying stale states.
- Team has no state named like the map: validation at enable time lists missing names.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Tracker + direction | Linear only, two-way (pull to drafts; push state + completion comment). |
