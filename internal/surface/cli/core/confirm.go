package core

import (
	"fmt"
	"strings"
)

// Refusal describes what Confirm returns when it cannot ask.
type Refusal struct {
	// Cmd names the command in the message ("rm").
	Cmd string
	// Flag is the flag that skips the question ("-force").
	Flag string
	// What the flag lets the command do without asking ("delete").
	What string
	// Hint is the command to run instead ("rex rm <sel> -force").
	Hint string
	// Extra is appended to the message ("or -dry-run to list").
	Extra string
	// DefaultYes asks [Y/n] and treats an empty answer as yes.
	DefaultYes bool
}

// Confirm asks question on stderr and reads a y/yes answer from stdin. It
// asks only in the text format with stdin and stderr both terminals; in any
// other case it does not prompt, and returns ExitOperationRefused (6) naming
// the flag that skips the question, so a script or an agent never blocks and
// never deletes by accident. A declined question returns false and no error;
// the caller reports it.
func (p *Printer) Confirm(question string, r Refusal) (bool, error) {
	if p.Format != FormatText || !p.InTTY || !p.ErrTTY {
		why := "stdin is not a terminal"
		switch {
		case p.InTTY && p.ErrTTY:
			why = "cannot ask in " + p.Format.String() + " mode"
		case p.InTTY:
			why = "stderr is not a terminal"
		}
		msg := fmt.Sprintf("%s: %s; pass %s to %s without asking", r.Cmd, why, r.Flag, r.What)
		if r.Extra != "" {
			msg += ", " + r.Extra
		}
		return false, Errorf(ExitOperationRefused, r.Hint, "%s", msg)
	}
	choice := "[y/N]"
	if r.DefaultYes {
		choice = "[Y/n]"
	}
	_, _ = fmt.Fprintf(p.Err, "%s %s ", question, choice)
	var ans string
	_, _ = fmt.Fscanln(p.In, &ans)
	a := strings.ToLower(ans)
	return a == "y" || a == "yes" || (r.DefaultYes && a == ""), nil
}
