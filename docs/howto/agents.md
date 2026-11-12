# Drive rex from an agent

rex has a mode for LLM agents: one line per record, no color, no prompts,
one error line on stderr. It turns on by itself inside Claude Code (a Bash
tool call, where `CLAUDECODE=1` and stdout is not a terminal) and with
`REX_AGENT=1` or `--agent` anywhere else.

## Let the agent find the card

`rex agent-help` prints a card of about 30 lines: the line format, exit
codes, the commands to read and write with, and the interactive ones to
avoid. `rex --help` prints the same card in agent mode.

## Paste into your CLAUDE.md or AGENTS.md

rex never writes to your files. This snippet is enough:

```md
## Supervising coding agents with rex

Run `rex agent-help` once for the usage card. Output is one `kind key=value`
line per record and ends in `ok cmd=<c> …` on stdout; an error is one
`error code=N kind=K msg=… hint=…` line on stderr. Read with `rex ls`,
`rex status`, `rex log <sel> -n 50`; wait with `rex wait <sel> -until
needs_input -timeout 10m`; start with `rex new -tool claude -model opus --
"<prompt>"`; answer with `rex reply <sel> -- "<text>"`. Destructive commands
need `-force`/`-yes` (`rex rm <sel> -force`). Never run bare `rex`,
`rex attach` or `rex setup` without `-yes`: they are interactive and exit 5
here. `rex errors -n 5` shows why a recent command failed.
```

## Exit codes

`0` ok, `1` error (also `rex status` when a session needs input), `2` not
found, `3` ambiguous selector, `4` daemon not running, `5` bad arguments,
`6` refused (needs `-force`/`-yes`), `7` timeout.

## Turning it off

`REX_AGENT=0` for one run; `cli_agent_auto: false` in Settings, Features to
stop the automatic switch; an explicit `-o text` or `-o json` always wins.
See [../cli.md](../cli.md#agent-mode).
