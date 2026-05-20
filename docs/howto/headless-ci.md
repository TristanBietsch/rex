# How to run sessions headless

Prerequisite: [quickstart.md](../quickstart.md).

## Goal

Spawn and monitor a session from a script without the TUI.

## Steps

1. Ensure the daemon is running:

```sh
rex daemon status || rex daemon start
```

2. Spawn:

```sh
rex new "fix the test" --tool echo --model short --slug ci-job-1
```

Note the short id printed on the first line.

3. Wait for completion:

```sh
rex wait ci-job-1 --until done --timeout 5m
```

Exit **7** on timeout. Exit **2** if the selector does not match.

4. Check aggregate status:

```sh
rex status
```

Exit **1** if any session is `needs_input`.

5. Read output:

```sh
rex log ci-job-1 -n 100
```

## JSON

```sh
rex ls --json
rex status --json
```

## See also

- [cli.md](../cli.md) — exit codes and selectors
- [protocol.md](../protocol.md) — integrator wire format
