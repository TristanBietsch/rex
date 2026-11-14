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
	out := core.AddOutputFlags(fs, core.AllFormats)
	raw := fs.Bool("raw", false, "do not append newline")
	rest, err := out.ParseText(fs, args, 1)
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
		if core.Out().InTTY {
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
		err = c.SendInput(sess.ID, []byte(text))
	} else {
		err = c.Reply(sess.ID, text)
	}
	if err != nil {
		return err
	}
	return emitAction("reply", replyData{refOf(sess), len(text), *raw}, "replied to "+sess.Slug, "",
		sessionFields(sess, core.Field{Key: "bytes", Val: len(text)}))
}

// replyData is the `-o json` payload of rex reply. Bytes is the length of
// the text, without the newline reply adds.
type replyData struct {
	actionData
	Bytes int  `json:"bytes"`
	Raw   bool `json:"raw"`
}
