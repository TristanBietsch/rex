package session

import (
	"flag"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// nameArgs is the parsed form of `rex name`.
type nameArgs struct {
	socket string
	sel    string
	name   string
	clear  bool
	show   bool
}

func parseNameArgs(args []string) (nameArgs, error) {
	fs := flag.NewFlagSet("name", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	clear := fs.Bool("clear", false, "clear the name (board shows the slug)")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return nameArgs{}, err
	}
	if err := core.CheckArgs(pos, 1, -1, "rex name <sel> [text…] | rex name <sel> -clear (text starting with - goes after --)"); err != nil {
		return nameArgs{}, err
	}
	a := nameArgs{socket: *socket, sel: pos[0], clear: *clear}
	text := strings.Join(pos[1:], " ")
	switch {
	case a.clear && text != "":
		return nameArgs{}, core.NewExitError(core.ExitInvalidArgs, "name: -clear takes no text")
	case !a.clear && text == "":
		a.show = true
	}
	if !a.show && !a.clear {
		n, err := protocol.NormalizeSessionName(text)
		if err != nil {
			return nameArgs{}, core.NewExitError(core.ExitInvalidArgs, "name: "+err.Error())
		}
		if n == "" {
			return nameArgs{}, core.NewExitError(core.ExitInvalidArgs, "name: empty name (use -clear)")
		}
		a.name = n
	}
	return a, nil
}

// RunName sets, clears, or prints a session's display name. Unlike `rename`
// (which changes the slug selector) the name is free text shown on the board.
func RunName(args []string) error {
	a, err := parseNameArgs(args)
	if err != nil {
		return err
	}
	c, err := client.Dial(a.socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, a.sel)
	if err != nil {
		return err
	}
	if a.show {
		if sess.Name == "" {
			fmt.Printf("(no name — board shows slug %s)\n", sess.Slug)
			return nil
		}
		fmt.Println(sess.Name)
		return nil
	}
	reqID := "cli-name-" + sess.ShortID
	if err := c.SetSessionName(sess.ID, a.name, reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := awaitPatch(c, sess.ID, reqID, "name"); err != nil {
		return err
	}
	if a.clear {
		fmt.Printf("%s: name cleared\n", sess.Slug)
	} else {
		fmt.Printf("%s: named %q (%d/%d)\n", sess.Slug, a.name, utf8.RuneCountInString(a.name), protocol.MaxSessionNameRunes)
	}
	return nil
}
