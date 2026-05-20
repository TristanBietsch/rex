package session

import (
	"flag"
	"io"
	"os"
	"strings"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// RunReply sends a one-shot text reply (newline-terminated) to a session.
func RunReply(args []string) error {
	fs := flag.NewFlagSet("reply", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	raw := fs.Bool("raw", false, "do not append newline")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if fs.NArg() < 1 {
		return core.NewExitError(core.ExitInvalidArgs, "reply: selector required")
	}
	sel := fs.Arg(0)

	var text string
	if fs.NArg() > 1 {
		text = strings.Join(fs.Args()[1:], " ")
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
		text = string(b)
	}

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
	if err != nil {
		return err
	}

	if *raw {
		return c.SendInput(sess.ID, []byte(text))
	}
	return c.Reply(sess.ID, text)
}
