package session

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunNew spawns a new agent session non-interactively.
func RunNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	tool := fs.String("tool", "echo", "tool id")
	model := fs.String("model", "short", "model id within tool")
	effort := fs.String("effort", "", "reasoning effort")
	slug := fs.String("slug", "", "session slug (defaults to derived from prompt)")
	cwd := fs.String("cwd", "", "working directory for the agent (defaults to $PWD)")
	fleet := fs.String("fleet", "", "fleet name (optional; groups related sessions)")
	noAttach := fs.Bool("no-attach", true, "spawn and exit (default)")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	_ = noAttach

	if *cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
		*cwd = wd
	}

	// Prompt = positional args joined, or stdin if not a TTY
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		fi, _ := os.Stdin.Stat()
		if (fi.Mode() & os.ModeCharDevice) == 0 {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return core.NewExitError(core.ExitGeneric, err.Error())
			}
			prompt = string(b)
		}
	}

	if *slug == "" {
		if prompt != "" {
			*slug = deriveSlug(prompt)
		} else {
			*slug = "session"
		}
	}

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	if _, err := c.Hello("rex-cli"); err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}

	req := protocol.NewSession{
		ToolID: *tool, ModelID: *model, Effort: *effort,
		Slug: *slug, CWD: *cwd, InitialPrompt: prompt,
		Fleet: *fleet,
	}
	if err := c.NewSession(req); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}

	// Wait for SessionAdded to surface the new id.
	if err := c.SetReadDeadline(core.DeadlineSeconds(5)); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	for {
		env, err := c.NextEvent()
		if err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
		switch env.Type {
		case protocol.EventSessionAdded:
			var sum protocol.SessionSummary
			if err := json.Unmarshal(env.Data, &sum); err != nil {
				return core.NewExitError(core.ExitGeneric, err.Error())
			}
			if _, err := os.Stdout.WriteString(sum.ShortID + "\t" + sum.Slug + "\n"); err != nil {
				return core.NewExitError(core.ExitGeneric, err.Error())
			}
			return nil
		case protocol.EventError:
			var ee protocol.ErrorEvent
			_ = json.Unmarshal(env.Data, &ee)
			return core.NewExitError(core.ExitGeneric, "spawn failed: "+ee.Message)
		}
	}
}

func deriveSlug(prompt string) string {
	s := prompt
	if len(s) > 32 {
		s = s[:32]
	}
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
