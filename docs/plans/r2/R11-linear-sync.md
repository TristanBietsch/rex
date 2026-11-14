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

## R11-0 spike findings (2026-11-11, docs only)

Sources: Linear developer docs (GraphQL, rate limiting, pagination, filtering, webhooks). No live call made yet: the read-only prototype needs a test team and API key from the user.

| Topic | Finding | Effect on the design |
|---|---|---|
| Endpoint | `https://api.linear.app/graphql` | one client |
| Auth | personal API key: `Authorization: <API_KEY>` (no `Bearer`); OAuth2: `Authorization: Bearer <token>` | start with a personal key in the keychain; OAuth app later only if needed |
| Read issues | `issues(filter: …, first: 50, after: cursor, orderBy: updatedAt)`; Relay cursors, `pageInfo { hasNextPage endCursor }`, default page 50; `nodes { … }` shorthand | paginate until `hasNextPage` is false |
| Filters | `team: { key: { eq: "ENG" } }`, `labels: { name: { eq: "rex" } }`, `assignee: { isMe: { eq: true } }`, `state: { type: { nin: ["completed","canceled"] } }`, `updatedAt: { gt: <ISO time> }`; `and` by default, `or: [ … ]` | incremental pull: only issues updated since the last sync |
| Workflow states | `workflowStates(filter: { team: { key: { eq: "ENG" } } })` → `id name type`; types: triage, backlog, unstarted, started, completed, canceled | `linear_state_map` maps rex stages to state **names**, resolved to ids per team at enable time; "done" detection uses `type`, not names |
| Update state | `issueUpdate(id: "ENG-123", input: { stateId })` (id or key) | push |
| Comment | `commentCreate(input: { issueId, body })`, markdown body | completion comment |
| Rate limits | API key: 2,500 requests/h and 3,000,000 complexity points/h per user; one query ≤ 10,000 points; headers `X-RateLimit-Requests-Remaining`, `X-RateLimit-Complexity-Remaining`, `…-Reset` (epoch ms); over the limit: HTTP 429, GraphQL error `RATELIMITED`, `Retry-After` | poll every 2 min ≈ 30 requests/h; back off on 429 using `Retry-After`; pause sync when remaining < 10 % |
| Webhooks | HMAC-SHA256 `Linear-Signature`; endpoint must be a **public HTTPS, non-localhost URL**; needs `admin` scope | **not usable** for a local daemon: polling it is |

Design changes from the spike:
- Pull is incremental (`updatedAt > last_sync`, ordered by `updatedAt`), stored per team in `~/.local/state/rex/linear.json`.
- The state map is resolved to state ids once per team and re-resolved on a "state not found" error.
- Still to verify with a live key: whether personal API keys can be limited to read + issue update + comment, and the exact complexity cost of the pull query.

## Plan
0. **R11-0 Spike.** Docs part done (findings above). Remaining: one read-only pull against a test team, with a key the user provides through an env var (never pasted into chat or files).
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
