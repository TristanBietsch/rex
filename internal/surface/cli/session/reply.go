package session

import (
	"flag"
	"io"
	"os"
	"strings"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunReply sends a one-shot text reply (newline-terminated) to a session.
func RunReply(args []string) error {
	fs := flag.NewFlagSet("reply", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	raw := fs.Bool("raw", false, "do not append newline")
	rest, err := core.ParseTextArgs(fs, args, 1)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return core.NewExitError(core.ExitInvalidArgs, "reply: selector required")
	}
	sel := rest[0]

	var text string
	if len(rest) > 1 {
		text = strings.Join(rest[1:], " ")
	} else {
		// A terminal would block here until the user types ctrl+d.
		if pruneIO.interactive() {
			return core.NewExitError(core.ExitInvalidArgs,
				"reply: no text; pass it as arguments or pipe it on stdin (`rex reply <sel> -- text`)")
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
		text = string(b)
	}

	c, err := socket.Dial()
	if err != nil {
		return err
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
