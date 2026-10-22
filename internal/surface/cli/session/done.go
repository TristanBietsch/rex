package session

import (
	"flag"
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// defaultRetireAfter reads retire_after_hours from config.yaml (the same
// setting the TUI uses), falling back to 7 days.
func defaultRetireAfter() time.Duration {
	st := settings.NewStore()
	_ = st.Load(settings.DefaultPath())
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
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	after := fs.Duration("after", 0, "clear after this long (e.g. 2h, 72h); default retire_after_hours")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "done: <selector> [-after 72h]")
	}
	ttl := *after
	if ttl == 0 {
		ttl = defaultRetireAfter()
	}
	if ttl < 10*time.Second {
		return core.NewExitError(core.ExitInvalidArgs, "done: -after must be at least 10s")
	}
	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, fs.Arg(0))
	if err != nil {
		return err
	}
	if !sess.RetireAt.IsZero() {
		fmt.Printf("%s: already marked done — clears in %s (rex undone to cancel)\n", sess.Slug, shortDuration(time.Until(sess.RetireAt)))
		return nil
	}
	reqID := "cli-done-" + sess.ShortID
	if err := c.Retire(sess.ID, ttl, reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := awaitPatch(c, sess.ID, reqID, "retire_at"); err != nil {
		return err
	}
	fmt.Printf("%s: marked done — clears in %s (rex undone %s to cancel)\n", sess.Slug, shortDuration(ttl), sess.ShortID)
	return nil
}

// RunUndone cancels a pending `rex done`.
func RunUndone(args []string) error {
	fs := flag.NewFlagSet("undone", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "undone: <selector>")
	}
	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, fs.Arg(0))
	if err != nil {
		return err
	}
	if sess.RetireAt.IsZero() {
		fmt.Printf("%s: not marked done\n", sess.Slug)
		return nil
	}
	reqID := "cli-undone-" + sess.ShortID
	if err := c.Unretire(sess.ID, reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := awaitPatch(c, sess.ID, reqID, "retire_at"); err != nil {
		return err
	}
	fmt.Printf("%s: unmarked\n", sess.Slug)
	return nil
}
