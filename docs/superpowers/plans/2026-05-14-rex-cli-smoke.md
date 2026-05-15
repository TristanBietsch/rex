# Plan B smoke test

Ten-minute manual check that the CLI is alive and behaves.

## Setup

```sh
make build-all
rm -f /tmp/rex.sock; rm -rf /tmp/rex-state
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state &
DAEMON=$!
sleep 0.5
export REX_SOCKET=/tmp/rex.sock
```

## 1. Status (no sessions)

```sh
./rex status --socket $REX_SOCKET
```

Expect `0 awaiting input · 0 working · 0 completed`. Exit code 0.

## 2. Spawn + watch + log

```sh
./rex new --socket $REX_SOCKET --tool echo --model short --slug demo --cwd /tmp
./rex ls --socket $REX_SOCKET
sleep 4
./rex ls --socket $REX_SOCKET
./rex log --socket $REX_SOCKET --state-dir /tmp/rex-state demo
```

Expect `rex new` to print `<short-id>\tdemo`. After 4s, `rex ls` shows the demo session in `done` state. `rex log` prints the transcript.

## 3. Attach to a long session

```sh
./rex new --socket $REX_SOCKET --tool echo --model long --slug attach-me --cwd /tmp
sleep 1
timeout 10 ./rex attach --socket $REX_SOCKET attach-me || true
```

You should see `step 1` through `step 5` stream by. Press ctrl+a d to detach early.

## 4. Reply to a waiting session

```sh
./rex new --socket $REX_SOCKET --tool echo --model prompt --slug ask --cwd /tmp
sleep 2
./rex ls --socket $REX_SOCKET --state needs_input
./rex reply --socket $REX_SOCKET ask "hello back"
sleep 1
./rex log --socket $REX_SOCKET --state-dir /tmp/rex-state ask | tail -5
```

Expect to see "got: hello back" in the transcript.

## 5. Wait

```sh
./rex new --socket $REX_SOCKET --tool echo --model long --slug waiter --cwd /tmp
./rex wait --socket $REX_SOCKET waiter --until done --timeout 30s
echo "exit code: $?"
```

Exit code 0 after ~5 seconds.

## 6. Concurrency cap

```sh
kill $DAEMON; wait $DAEMON 2>/dev/null
./rex-daemon --socket /tmp/rex.sock --state-dir /tmp/rex-state --max-concurrent-sessions 1 &
DAEMON=$!
sleep 0.5

./rex new --socket $REX_SOCKET --tool echo --model long --slug cap-1 --cwd /tmp
./rex new --socket $REX_SOCKET --tool echo --model long --slug cap-2 --cwd /tmp
./rex ls --socket $REX_SOCKET
```

Expect only `cap-1` to appear in `rex ls` — the second `rex new` should print an error (exit non-zero) about "too many concurrent sessions".

## 7. Cleanup

```sh
./rex daemon stop
```

## Acceptance

Plan B is **done** when steps 1–7 succeed end-to-end and `make test` is green with the race detector clean.
