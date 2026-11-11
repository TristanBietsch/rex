package session

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunNew spawns a new agent session non-interactively.
func RunNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	tool := fs.String("tool", "echo", "tool id")
	model := fs.String("model", "short", "model id within tool")
	effort := fs.String("effort", "", "reasoning effort")
	slug := fs.String("slug", "", "session slug (defaults to derived from prompt)")
	cwd := fs.String("cwd", "", "working directory for the agent (defaults to $PWD)")
	fleet := fs.String("fleet", "", "fleet name (optional; groups related sessions)")
	noAttach := fs.Bool("no-attach", true, "spawn and exit (default)")
	rest, err := core.ParseTextArgs(fs, args, 0)
	if err != nil {
		return err
	}
	_ = noAttach

	absCwd, err := resolveNewCwd(*cwd)
	if err != nil {
		return err
	}
	*cwd = absCwd

	// Prompt = positional args joined, or stdin if not a TTY
	prompt := strings.Join(rest, " ")
	if prompt == "" {
		var err error
		if prompt, err = promptFromStdin(os.Stdin); err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
	}

	// An explicit -slug is sent as-is (the daemon rejects one already in
	// use); a derived one is made unique against the snapshot below.
	derived := *slug == ""
	if derived {
		if prompt != "" {
			*slug = deriveSlug(prompt)
		}
		if *slug == "" {
			*slug = "session"
		}
	} else if err := protocol.ValidateSlug(*slug); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, "new: "+err.Error())
	}

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	if derived {
		*slug = uniqueSlug(*slug, snap.Sessions)
	}

	req := protocol.NewSession{
		ToolID: *tool, ModelID: *model, Effort: *effort,
		Slug: *slug, Title: prompt, CWD: *cwd, InitialPrompt: prompt,
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
			slog.Debug("cli: new rejected", "slug", *slug, "code", ee.Code, "message", ee.Message)
			code := core.ExitGeneric
			if ee.Code == protocol.ErrCodeBadIntent {
				code = core.ExitInvalidArgs
			}
			return core.NewExitError(code, "spawn failed: "+ee.Message)
		}
	}
}

// promptFromStdin reads the prompt from in unless in is a terminal. A closed
// or missing stdin has no prompt.
func promptFromStdin(in *os.File) (string, error) {
	if in == nil || core.IsTerminal(in) {
		return "", nil
	}
	b, err := io.ReadAll(in)
	if errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF) {
		return "", nil
	}
	return string(b), err
}

// resolveNewCwd turns -cwd into the absolute path the daemon requires: empty
// is the working directory, anything else is made absolute here, not against
// the daemon's own working directory. It must exist and be a folder.
func resolveNewCwd(cwd string) (string, error) {
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", core.NewExitError(core.ExitGeneric, err.Error())
		}
		return wd, nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", core.NewExitError(core.ExitInvalidArgs, "new: -cwd "+cwd+": "+err.Error())
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", core.NewExitError(core.ExitInvalidArgs, "new: -cwd "+cwd+": "+err.Error())
	}
	if !fi.IsDir() {
		return "", core.NewExitError(core.ExitInvalidArgs, "new: -cwd "+cwd+" is not a folder")
	}
	return abs, nil
}

// uniqueSlug returns base, or base-2, base-3, … — the first not already
// used by a session in the snapshot.
func uniqueSlug(base string, sessions []protocol.SessionSummary) string {
	taken := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		taken[s.Slug] = struct{}{}
	}
	if _, ok := taken[base]; !ok {
		return base
	}
	for n := 2; ; n++ {
		c := base + "-" + strconv.Itoa(n)
		if _, ok := taken[c]; !ok {
			return c
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
