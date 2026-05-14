# Plan A smoke test

Five-minute manual check that the daemon is alive and behaves.

## Setup

```sh
make build
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state
```

In another terminal:

## 1. Hello → Snapshot

```sh
( printf '{"v":1,"kind":"Intent","type":"Hello","id":"h","data":{"client_version":"manual"}}\n'; sleep 0.2 ) \
  | socat - UNIX-CONNECT:/tmp/rex.sock | head -1 | jq
```

Expect a `Snapshot` event with `sessions: []`.

## 2. Spawn an echo session

```sh
( printf '{"v":1,"kind":"Intent","type":"Hello","id":"h","data":{}}\n'; \
  printf '{"v":1,"kind":"Intent","type":"NewSession","id":"n","data":{"tool_id":"echo","model_id":"short","slug":"smoke","cwd":"/tmp"}}\n'; \
  sleep 4 ) \
  | socat - UNIX-CONNECT:/tmp/rex.sock | jq -c '.type'
```

Expect, in order:
- `"Snapshot"`
- `"SessionAdded"`
- `"SessionUpdated"` (state → working)
- `"SessionUpdated"` (last_line updates as echo runs)
- `"SessionUpdated"` (state → done)

## 3. Verify persistence

```sh
ls /tmp/rex-state/sessions/
cat /tmp/rex-state/sessions/*/meta.json | jq
cat /tmp/rex-state/sessions/*/transcript.log
```

Expect one session dir, a `meta.json` with `state: "done"`, and a transcript with the echo output.

## 4. Verify "crashed" recovery

Kill the daemon (ctrl+c), then restart and verify the prior session reloads as `crashed`. (Spawn a `long` session first if you want to see a state actually flip from working to crashed.)
