package core

import (
	"flag"
	"strconv"
)

// agentCard is what `rex agent-help` and `rex --help` print in agent mode:
// a compact usage card for LLM agents. Keep it under 30 lines.
const agentCard = `rex agent card v1: supervise coding-agent sessions (claude, agy) from a shell.
mode: --agent or REX_AGENT=1. One line per record, no color, never prompts.
line: <kind> key=value…  bare [A-Za-z0-9._:/@+%-]+ or "quoted"; empty ""
end:  stdout "ok cmd=<c> …" | stderr "error code=N kind=K msg=… hint=…"
exit: 0 ok 1 error 2 not_found 3 ambiguous 4 no_daemon 5 bad_args 6 refused 7 timeout
sel:  short id (4+ hex) | slug | full id
read  rex ls [-state S] [-tool T]   -> session id state tool model effort slug idle_s name
      rex status                    -> ok needs_input working queued done failed (exit 1 if needs_input>0)
      rex log <sel> -n 50           -> transcript text, ANSI stripped
      rex wait <sel> -until done|needs_input|any -timeout 10m
write rex new -tool claude -model opus [-cwd D] -- <prompt> -> ok cmd=new id slug
      rex reply <sel> -- <text> | rex send <sel> -- <text>
      rex done <sel> | rex resume <sel> | rex rm <sel> -force | rex prune -yes
never rex (TUI), rex attach, rex setup without -yes, rex onboard without -check: interactive, exit 5 here
more  rex <cmd> -h · rex errors -n 5 · formats: -o json|txt|md
`

// agentCardData is the `-o json` payload of rex agent-help: the card as data.
type agentCardData struct {
	Card      int               `json:"card"`
	Mode      string            `json:"mode"`
	Formats   []string          `json:"formats"`
	Exit      map[string]string `json:"exit"`
	Kinds     []string          `json:"kinds"`
	Selectors string            `json:"selectors"`
	Commands  []commandData     `json:"commands"`
}

func agentCardResult() Result {
	kinds := make([]string, 0, 8)
	exit := map[string]string{}
	for _, code := range []int{ExitOK, ExitGeneric, ExitSelectorNotFound, ExitAmbiguousSelector,
		ExitDaemonUnreachable, ExitInvalidArgs, ExitOperationRefused, ExitWaitTimedOut} {
		k := Kind(code)
		kinds = append(kinds, k)
		exit[strconv.Itoa(code)] = k
	}
	return Result{
		Cmd: "agent-help",
		Data: agentCardData{
			Card:      1,
			Mode:      "--agent, -o agent or REX_AGENT=1: one line per record, an ok trailer on stdout, one error line on stderr, no color, no prompts",
			Formats:   formatNamesOf(AllFormats),
			Exit:      exit,
			Kinds:     kinds,
			Selectors: "short id (4+ hex), slug or full id",
			Commands:  commandsData(),
		},
		Content: agentCard,
	}
}

func emitAgentCard(p *Printer) error { return p.Emit(agentCardResult()) }

// RunAgentHelp prints the agent card (`rex agent-help`).
func RunAgentHelp(args []string) error {
	fs := flag.NewFlagSet("agent-help", flag.ContinueOnError)
	out := AddOutputFlags(fs, AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return NewExitError(ExitInvalidArgs, "agent-help: no arguments")
	}
	return emitAgentCard(Out())
}
