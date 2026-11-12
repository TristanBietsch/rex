package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/handoff"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// HotConfig wires a hot restart (R20): the daemon re-execs its own binary
// in place, keeping the listener, the state lock and every session's PTY
// open across execve. nil Config.Hot: Restart answers unsupported.
type HotConfig struct {
	// Exe is the binary to exec: the daemon's own executable path.
	Exe string
	// LockFD is the state lock's descriptor.
	LockFD int
	// Argv builds the new image's arguments for a handoff file.
	Argv func(handoffPath string) []string
	// Preflight runs the new binary against a probe handoff file and fails
	// when it cannot take one over (missing, older build, other format).
	Preflight func(ctx context.Context, exe, probePath string) error
	// Exec replaces the process image; it returns only on failure.
	// handoff.Exec in production.
	Exec func(exe string, argv, env []string) error
}

// freezeBudget bounds how long all sessions may take to park.
const freezeBudget = 3 * time.Second

// preflightBudget bounds the new binary's -handoff-check run.
const preflightBudget = 10 * time.Second

// AdoptSession is a session handed over by the previous daemon image,
// supervised from Serve's start (Config.Adopt).
type AdoptSession struct {
	Session    *state.Session
	Adopted    pty.Adopted
	Cols, Rows uint16
}

var errHotUnsupported = errors.New("hot restart is not supported here; use rex daemon restart -cold")

// RegisterFreeze stores a session's FreezeFunc for a hot restart.
func (s *Server) RegisterFreeze(sessionID string, fn pty.FreezeFunc) {
	s.freezeMu.Lock()
	defer s.freezeMu.Unlock()
	if s.freezeFuncs == nil {
		s.freezeFuncs = make(map[string]pty.FreezeFunc)
	}
	s.freezeFuncs[sessionID] = fn
}

// UnregisterFreeze drops a session's FreezeFunc.
func (s *Server) UnregisterFreeze(sessionID string) {
	s.freezeMu.Lock()
	defer s.freezeMu.Unlock()
	delete(s.freezeFuncs, sessionID)
}

// liveWithoutFreeze lists live sessions that cannot be frozen (a session
// still starting, a platform without readiness waits).
func (s *Server) liveWithoutFreeze() []string {
	s.freezeMu.Lock()
	defer s.freezeMu.Unlock()
	var out []string
	for _, sum := range s.cfg.Store.Snapshot() {
		switch sum.State {
		case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
			if _, ok := s.freezeFuncs[sum.ID]; !ok {
				out = append(out, sum.ID)
			}
		default:
		}
	}
	return out
}

// FreezeAll freezes every session with a FreezeFunc. It is all or nothing:
// when one fails, the frozen ones are thawed and the error names it.
func (s *Server) FreezeAll(ctx context.Context) ([]handoff.Session, func(), error) {
	s.freezeMu.Lock()
	ids := make([]string, 0, len(s.freezeFuncs))
	for id := range s.freezeFuncs {
		ids = append(ids, id)
	}
	funcs := make(map[string]pty.FreezeFunc, len(ids))
	for _, id := range ids {
		funcs[id] = s.freezeFuncs[id]
	}
	s.freezeMu.Unlock()
	sort.Strings(ids)

	var thaws []func()
	thawAll := func() {
		for _, t := range thaws {
			t()
		}
	}
	out := make([]handoff.Session, 0, len(ids))
	for _, id := range ids {
		fz, thaw, err := funcs[id](ctx)
		if err != nil {
			thawAll()
			return nil, func() {}, fmt.Errorf("session %s: %w", s.shortOf(id), err)
		}
		thaws = append(thaws, thaw)
		out = append(out, handoff.Session{ID: id, PID: fz.PID, FD: fz.FD, Cols: fz.Cols, Rows: fz.Rows, Screen: fz.Screen})
	}
	return out, thawAll, nil
}

func (s *Server) shortOf(id string) string {
	if sess, ok := s.cfg.Store.Get(id); ok && sess.ShortID != "" {
		return sess.ShortID
	}
	return id
}

// pauseAccept stops taking connections: new ones wait in the listen
// backlog (for the next image, or until resumeAccept).
func (s *Server) pauseAccept() {
	s.acceptMu.Lock()
	defer s.acceptMu.Unlock()
	if s.acceptPaused != nil {
		return
	}
	s.acceptPaused = make(chan struct{})
	if l := s.listener; l != nil {
		_ = l.SetDeadline(time.Unix(1, 0))
	}
}

// resumeAccept undoes pauseAccept.
func (s *Server) resumeAccept() {
	s.acceptMu.Lock()
	defer s.acceptMu.Unlock()
	if s.acceptPaused == nil {
		return
	}
	if l := s.listener; l != nil {
		_ = l.SetDeadline(time.Time{})
	}
	close(s.acceptPaused)
	s.acceptPaused = nil
}

// acceptGate returns the channel Serve waits on while accepting is paused,
// nil when it is not.
func (s *Server) acceptGate() <-chan struct{} {
	s.acceptMu.Lock()
	defer s.acceptMu.Unlock()
	return s.acceptPaused
}

// dropOtherConns closes every client connection except keep (the one that
// asked for the restart) and waits, at most d, for their handlers to end,
// so no client input is on its way into a session while it freezes.
func (s *Server) dropOtherConns(keep *clientConn, d time.Duration) {
	s.connsMu.Lock()
	conns := make([]*clientConn, 0, len(s.conns))
	for c := range s.conns {
		if c != keep {
			conns = append(conns, c)
		}
	}
	s.connsMu.Unlock()
	for _, c := range conns {
		c.abort(errServerShutdown)
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s.connsMu.Lock()
		left := 0
		for c := range s.conns {
			if c != keep {
				left++
			}
		}
		s.connsMu.Unlock()
		if left == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// HotRestart re-execs the daemon in place with every live session handed
// over. On success it does not return. On failure the daemon is as before
// (sessions thawed, accepting again) and the error says why. requester is
// the connection that asked (kept open to hear a failure); nil for none.
func (s *Server) HotRestart(ctx context.Context, requester *clientConn) error {
	h := s.cfg.Hot
	if h == nil || !handoff.Supported || s.listener == nil {
		return errHotUnsupported
	}
	if !s.hotMu.TryLock() {
		return errors.New("a restart is already running")
	}
	defer s.hotMu.Unlock()
	if ids := s.liveWithoutFreeze(); len(ids) > 0 {
		short := make([]string, len(ids))
		for i, id := range ids {
			short[i] = s.shortOf(id)
		}
		return fmt.Errorf("session(s) %v cannot be handed over yet (still starting); retry in a few seconds", short)
	}

	pid := os.Getpid()
	// Preflight first, before anything is frozen: the new binary must read
	// this build's handoff format.
	probe := handoff.Path(s.cfg.StateDir, pid) + ".probe"
	_ = os.Remove(probe)
	if err := handoff.Write(probe, handoff.File{Format: handoff.Format, PID: pid, UID: os.Getuid()}); err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, preflightBudget)
	err := h.Preflight(pctx, h.Exe, probe)
	cancel()
	_ = os.Remove(probe)
	if err != nil {
		return fmt.Errorf("new binary refused the handoff (%s): %w", h.Exe, err)
	}

	slog.Info("server: hot restart: quiescing")
	s.pauseAccept()
	s.dropOtherConns(requester, time.Second)
	fctx, cancelFreeze := context.WithTimeout(ctx, freezeBudget)
	sessions, thaw, err := s.FreezeAll(fctx)
	cancelFreeze()
	rollback := func() {
		thaw()
		s.resumeAccept()
	}
	if err != nil {
		s.resumeAccept()
		return fmt.Errorf("freeze: %w", err)
	}
	for _, hs := range sessions {
		if sess, ok := s.cfg.Store.Get(hs.ID); ok {
			if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
				rollback()
				return fmt.Errorf("persist %s: %w", sess.ShortID, err)
			}
		}
	}
	lf, err := s.listener.File()
	if err != nil {
		rollback()
		return fmt.Errorf("listener: %w", err)
	}
	defer lf.Close()
	file := handoff.File{
		Format: handoff.Format, PID: pid, UID: os.Getuid(), Build: s.cfg.Build,
		Socket: s.cfg.Socket, ListenerFD: int(lf.Fd()), LockFD: h.LockFD, //nolint:gosec // G115
		Sessions: sessions,
	}
	path := handoff.Path(s.cfg.StateDir, pid)
	_ = os.Remove(path) // a stale file of an earlier failed attempt
	if err := handoff.Write(path, file); err != nil {
		rollback()
		return err
	}
	fds := []int{file.ListenerFD, file.LockFD}
	for _, hs := range sessions {
		fds = append(fds, hs.FD)
	}
	restore, err := handoff.Inherit(fds...)
	if err != nil {
		_ = os.Remove(path)
		rollback()
		return err
	}
	slog.Info("server: hot restart: exec", "exe", h.Exe, "sessions", len(sessions))
	err = h.Exec(h.Exe, h.Argv(path), os.Environ())
	// Still here: the exec failed and this image goes on.
	restore()
	_ = os.Remove(path)
	rollback()
	slog.Error("server: hot restart: exec failed; still running", "err", err)
	return fmt.Errorf("exec %s: %w", h.Exe, err)
}

// startAdopted supervises the sessions handed over by the previous image.
func (s *Server) startAdopted(ctx context.Context) {
	for _, a := range s.cfg.Adopt {
		if err := adoptSession(ctx, s, a); err != nil {
			slog.Error("server: adopt session failed", "session", a.Session.ID, "err", err)
		}
	}
	s.cfg.Adopt = nil
}

// boardsLocked lists the board connections.
func (s *Server) boards() []*clientConn {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	var out []*clientConn
	for c := range s.conns {
		if c.board.Load() {
			out = append(out, c)
		}
	}
	return out
}

// BoardInfos lists the boards connected now, sorted by pid.
func (s *Server) BoardInfos() []protocol.BoardInfo {
	var out []protocol.BoardInfo
	for _, c := range s.boards() {
		out = append(out, c.boardInfo())
	}
	slices.SortFunc(out, func(a, b protocol.BoardInfo) int { return a.PID - b.PID })
	return out
}

// boardMeta is what a board said about itself in Hello.
type boardMeta struct {
	mu   sync.Mutex
	info protocol.BoardInfo
}

func (c *clientConn) setBoardInfo(i protocol.BoardInfo) {
	c.meta.mu.Lock()
	c.meta.info = i
	c.meta.mu.Unlock()
	c.board.Store(true)
}

func (c *clientConn) boardInfo() protocol.BoardInfo {
	c.meta.mu.Lock()
	defer c.meta.mu.Unlock()
	return c.meta.info
}

// SwapBoards sends a swap event to every board whose build is not build
// and returns those boards.
func (s *Server) SwapBoards(build string) []protocol.BoardInfo {
	var out []protocol.BoardInfo
	for _, c := range s.boards() {
		info := c.boardInfo()
		if build != "" && info.Build == build {
			continue
		}
		c.sendEvent(protocol.EventSwap, "", protocol.SwapBoards{Build: build})
		out = append(out, info)
	}
	return out
}
