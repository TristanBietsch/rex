package session

import (
	"flag"
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// defaultRetireAfter reads retire_after_hours from config.yaml (the same
// setting the TUI uses), falling back to 7 days.
func defaultRetireAfter() time.Duration {
	st, _, _ := settings.LoadDefault()
	if h, ok := st.Get("retire_after_hours").(int); ok && h > 0 {
		return time.Duration(h) * time.Hour
	}
	return 7 * 24 * time.Hour
}

// RunDone marks a session done without stopping it. It keeps running and
// stays usable; after -after (default: retire_after_hours) the daemon kills
// it, removes it, and deletes its transcript — unless it is unmarked
// (`rex undone`) or typed into first. A session still working at the
// deadline is cleared once it settles.
func RunDone(args []string) error {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	after := fs.Duration("after", 0, "clear after this long (e.g. 2h, 72h); default retire_after_hours")
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "done: <selector> [-after 72h]")
	}
	ttl := *after
	if ttl == 0 {
		ttl = defaultRetireAfter()
	}
	if ttl < 10*time.Second {
		return core.NewExitError(core.ExitInvalidArgs, "done: -after must be at least 10s")
	}
	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, rest[0])
	if err != nil {
		return err
	}
	if !sess.RetireAt.IsZero() {
		left := time.Until(sess.RetireAt)
		return emitAction("done", doneData{refOf(sess), "already_marked", int(left.Seconds())},
			sess.Slug+" is already marked done · clears in "+shortDuration(left),
			fmt.Sprintf("%s: already marked done — clears in %s (rex undone to cancel)", sess.Slug, shortDuration(left)),
			sessionFields(sess, core.Field{Key: "outcome", Val: "already_marked"}, core.Field{Key: "clears_in_s", Val: int(left.Seconds())}),
			core.Hint{Cmd: "rex undone " + sess.ShortID, Text: "cancel"})
	}
	reqID := "cli-done-" + sess.ShortID
	if err := c.Retire(sess.ID, ttl, reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := awaitPatch(c, sess.ID, reqID, "retire_at"); err != nil {
		return err
	}
	return emitAction("done", doneData{refOf(sess), "marked", int(ttl.Seconds())},
		"marked done "+sess.ShortID+" "+sess.Slug+" · clears in "+shortDuration(ttl),
		fmt.Sprintf("%s: marked done — clears in %s (rex undone %s to cancel)", sess.Slug, shortDuration(ttl), sess.ShortID),
		sessionFields(sess, core.Field{Key: "outcome", Val: "marked"}, core.Field{Key: "clears_in_s", Val: int(ttl.Seconds())}),
		core.Hint{Cmd: "rex undone " + sess.ShortID, Text: "cancel"})
}

// RunUndone cancels a pending `rex done`.
func RunUndone(args []string) error {
	fs := flag.NewFlagSet("undone", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "undone: <selector>")
	}
	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, rest[0])
	if err != nil {
		return err
	}
	if sess.RetireAt.IsZero() {
		return emitAction("undone", undoneData{refOf(sess), "not_marked"}, sess.Slug+" is not marked done",
			sess.Slug+": not marked done", sessionFields(sess, core.Field{Key: "outcome", Val: "not_marked"}))
	}
	reqID := "cli-undone-" + sess.ShortID
	if err := c.Unretire(sess.ID, reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := awaitPatch(c, sess.ID, reqID, "retire_at"); err != nil {
		return err
	}
	return emitAction("undone", undoneData{refOf(sess), "unmarked"}, "unmarked "+sess.ShortID+" "+sess.Slug,
		sess.Slug+": unmarked", sessionFields(sess, core.Field{Key: "outcome", Val: "unmarked"}))
}

// doneData is the `-o json` payload of rex done.
type doneData struct {
	actionData
	// Outcome is "marked" or "already_marked".
	Outcome string `json:"outcome"`
	// ClearsInS is the seconds until the daemon clears the session.
	ClearsInS int `json:"clears_in_s"`
}

// undoneData is the `-o json` payload of rex undone.
type undoneData struct {
	actionData
	// Outcome is "unmarked" or "not_marked".
	Outcome string `json:"outcome"`
}
