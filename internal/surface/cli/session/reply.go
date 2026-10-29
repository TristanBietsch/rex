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
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 1, -1, "rex reply <sel> [text…] [-raw] [-socket path] (text starting with - goes after --)"); err != nil {
		return err
	}
	sel := pos[0]

	var text string
	if len(pos) > 1 {
		text = strings.Join(pos[1:], " ")
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
