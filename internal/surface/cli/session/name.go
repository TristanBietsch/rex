package session

import (
	"flag"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// nameArgs is the parsed form of `rex name`.
type nameArgs struct {
	socket *core.SocketFlag
	sel    string
	name   string
	clear  bool
	show   bool
}

func parseNameArgs(args []string) (nameArgs, error) {
	fs := flag.NewFlagSet("name", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	clear := fs.Bool("clear", false, "clear the name (board shows the slug)")
	rest, err := out.ParseText(fs, args, 1)
	if err != nil {
		return nameArgs{}, err
	}
	if len(rest) < 1 {
		return nameArgs{}, core.NewExitError(core.ExitInvalidArgs, "name: <selector> [text…] | -clear <selector>")
	}
	a := nameArgs{socket: socket, sel: rest[0], clear: *clear}
	text := strings.Join(rest[1:], " ")
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
	c, err := a.socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, a.sel)
	if err != nil {
		return err
	}
	if a.show {
		text := sess.Name
		if text == "" {
			text = fmt.Sprintf("(no name — board shows slug %s)", sess.Slug)
		}
		p := core.Out()
		return p.Emit(core.Result{Cmd: "name", Data: nameData{refOf(sess), sess.Name},
			View: core.ActionView(core.Line{Text: text}, sessionFields(sess, core.Field{Key: "name", Val: sess.Name})...)})
	}
	reqID := "cli-name-" + sess.ShortID
	if err := c.SetSessionName(sess.ID, a.name, reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := awaitPatch(c, sess.ID, reqID, "name"); err != nil {
		return err
	}
	if a.clear {
		return emitAction("name", nameData{refOf(sess), ""}, "cleared the name of "+sess.Slug,
			sess.Slug+": name cleared", sessionFields(sess, core.Field{Key: "name", Val: ""}))
	}
	count := fmt.Sprintf("%d/%d", utf8.RuneCountInString(a.name), protocol.MaxSessionNameRunes)
	return emitAction("name", nameData{refOf(sess), a.name}, fmt.Sprintf("named %s %q (%s)", sess.Slug, a.name, count),
		fmt.Sprintf("%s: named %q (%s)", sess.Slug, a.name, count), sessionFields(sess, core.Field{Key: "name", Val: a.name}))
}

// nameData is the `-o json` payload of rex name.
type nameData struct {
	actionData
	Name string `json:"name"`
}
