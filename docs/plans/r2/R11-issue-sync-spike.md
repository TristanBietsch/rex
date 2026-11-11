# R11 Spike: Jira / Linear sync

## Goal
TODO: "Jira / linear sync ?". Decide whether rex cards should link to issues in a tracker, and in which direction.

## Options
| Option | What it does | Needs |
|---|---|---|
| A. Import as ToDo drafts (one-way in) | `rex issues pull --linear --team X --label rex` creates R09 draft cards with the issue title/body as the prompt and the issue key as a link | R09 drafts, API token in OS keychain or env var |
| B. Two-way status | card stage changes post status/comment to the issue (In review → "In Review", Done → comment with summary + PR link) | A + write scope token; mapping table stage ↔ status |
| C. Link only | `rex link <sel> LIN-123`; card shows key; `rex open` opens the URL. No API | nothing |

## Constraints
- Tokens never in `config.yaml`/Lua: env var or macOS Keychain / libsecret / Windows Credential Manager only.
- No network calls from the daemon unless the feature is enabled.
- Linear has a GraphQL API and an official MCP server; Jira Cloud has REST v3. Check rate limits and auth (OAuth vs personal token) in the spike.

## Method
1. Pick tracker(s) you actually use (decision below).
2. Read API docs for list/update issue + auth; prototype a read-only pull against a test workspace you provide (no writes).
3. Write build spec; it depends on R09-D3 = drafts.

## Decisions

| # | Question | Options (recommended first) |
|---|---|---|
| D1 | Tracker + direction | (a) Linear, import-only into ToDo drafts (A) first · (b) Linear + Jira, two-way (B) · (c) link only, no API (C). Dropping the item is also possible. |
