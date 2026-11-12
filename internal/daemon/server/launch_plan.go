package server

import (
	"log/slog"

	"github.com/tristanbietsch/rex/internal/daemon/winlaunch"
)

// launchEnv is the system the Windows launch plan reads; nil skips the plan
// (every platform but Windows). Tests set a fake one.
var launchEnv = func() *winlaunch.Env {
	if !winlaunch.Active {
		return nil
	}
	env := winlaunch.SystemEnv()
	return &env
}()

// planLaunch applies the Windows launch plan (winlaunch.Plan) to a child's
// argv: an npm .cmd shim is replaced by the program it runs, so cmd.exe
// never parses the prompt; a prompt that cannot stay on argv (an
// unresolvable shim, a command line over winlaunch.MaxCmdLine) is pasted
// into the PTY instead; anything else is refused. With env nil it returns
// its inputs unchanged.
func planLaunch(env *winlaunch.Env, session string, argv []string, ptyPrompt string, d promptDelivery) ([]string, string, promptDelivery, error) {
	if env == nil || len(argv) == 0 {
		return argv, ptyPrompt, d, nil
	}
	req := winlaunch.Request{Base: argv, Prompt: ptyPrompt}
	if d.promptArgs > 0 && d.promptArgs < len(argv) {
		req.Base = argv[:len(argv)-d.promptArgs]
		req.PromptArgs = argv[len(argv)-d.promptArgs:]
		req.Prompt = d.prompt
	}
	l, err := winlaunch.Plan(req, *env)
	if err != nil {
		slog.Warn("spawn: windows launch refused", "session", session, "err", err)
		return nil, "", d, err
	}
	if l.PromptMoved {
		// Bracketed paste once the agent's input is ready, as for a
		// prompt_paste tool.
		d = promptDelivery{}
		slog.Info("spawn: initial_prompt moved to pty paste", "session", session, "reason", l.Reason, "bytes", len(l.Paste))
	}
	slog.Info("spawn: windows launch", "session", session, "via", l.Via, "argc", len(l.Argv))
	return l.Argv, l.Paste, d, nil
}
