package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/tristanbietsch/rex/internal/daemon/handoff"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
)

// handoffCheckOK is what -handoff-check prints for a file it can take.
const handoffCheckOK = "ok " + handoff.Format

// runHandoffCheck is `rex-daemon -handoff-check <file>`: the old image's
// preflight. It parses the file and exits 0 only when this build can take
// it over.
func runHandoffCheck(path string) error {
	if err := handoff.Check(path); err != nil {
		return err
	}
	fmt.Println(handoffCheckOK)
	return nil
}

// preflight runs exe -handoff-check probe and requires handoffCheckOK.
func preflight(ctx context.Context, exe, probe string) error {
	cmd := exec.CommandContext(ctx, exe, "-handoff-check", probe) //nolint:gosec // G204: exe is this daemon's own executable.
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s (an older build cannot take a hot restart; use rex daemon restart -cold)", firstLine(msg))
	}
	if strings.TrimSpace(out.String()) != handoffCheckOK {
		return fmt.Errorf("unexpected answer %q", firstLine(out.String()))
	}
	return nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// handoffArgv is the new image's argv: this one's, without an earlier
// -handoff flag, plus -handoff path.
func handoffArgv(args []string, path string) []string {
	out := []string{os.Args[0]}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-handoff" || a == "--handoff":
			i++ // and its value
			continue
		case strings.HasPrefix(a, "-handoff=") || strings.HasPrefix(a, "--handoff="):
			continue
		}
		out = append(out, a)
	}
	return append(out, "-handoff", path)
}

// takeover is what the new image adopts from a handoff file.
type takeover struct {
	lock     *state.Lock
	listener *net.UnixListener
	sessions map[string]handoff.Session
}

// takeOver reads the handoff file left by the previous image of this
// process and adopts its lock and listener. The sessions' PTYs are wrapped
// later, per session (adoptions). A file that cannot be read is fatal:
// without it the inherited descriptors cannot be trusted.
func takeOver(path, stateDir, socket string) (*takeover, error) {
	f, err := handoff.Read(path, os.Getpid())
	if err != nil {
		return nil, err
	}
	if f.Socket != socket {
		return nil, fmt.Errorf("handoff: listener is for %s, not %s", f.Socket, socket)
	}
	if f.LockFD < 0 || f.ListenerFD < 0 {
		return nil, errors.New("handoff: bad descriptors")
	}
	lf := os.NewFile(uintptr(f.ListenerFD), "listener") //nolint:gosec // G115: checked >= 0.
	l, err := net.FileListener(lf)
	_ = lf.Close() // FileListener dups it
	if err != nil {
		return nil, fmt.Errorf("handoff: listener: %w", err)
	}
	ul, ok := l.(*net.UnixListener)
	if !ok {
		_ = l.Close()
		return nil, fmt.Errorf("handoff: listener is %T", l)
	}
	t := &takeover{lock: state.AdoptLock(stateDir, f.LockFD), listener: ul, sessions: map[string]handoff.Session{}}
	for _, s := range f.Sessions {
		t.sessions[s.ID] = s
	}
	slog.Info("daemon: hot restart: took over", "from_build", f.Build, "sessions", len(f.Sessions))
	return t, nil
}

// liveIDs is the set of handed-over session ids.
func (t *takeover) liveIDs() map[string]bool {
	if t == nil {
		return nil
	}
	ids := make(map[string]bool, len(t.sessions))
	for id := range t.sessions {
		ids[id] = true
	}
	return ids
}

// adoptions pairs each handed-over session with its loaded state. A
// handed-over session missing from disk has its child killed: nothing
// could supervise it.
func (t *takeover) adoptions(store *state.Store) []server.AdoptSession {
	if t == nil {
		return nil
	}
	var out []server.AdoptSession
	for id, hs := range t.sessions {
		file := os.NewFile(uintptr(hs.FD), "ptmx-"+id) //nolint:gosec // G115: descriptors fit.
		sess, ok := store.Get(id)
		if !ok || hs.FD < 0 || hs.PID <= 0 {
			slog.Error("daemon: hot restart: handed-over session has no state; stopping it", "session", id, "pid", hs.PID)
			killOrphan(hs.PID)
			if file != nil {
				_ = file.Close()
			}
			continue
		}
		out = append(out, server.AdoptSession{
			Session: sess, Cols: hs.Cols, Rows: hs.Rows,
			Adopted: pty.Adopted{File: file, PID: hs.PID, Screen: hs.Screen},
		})
	}
	return out
}
