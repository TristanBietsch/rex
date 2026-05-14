// Package pty supervises a single agent session: spawn, read, classify, persist.
package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/tristanbietsch/rex/internal/adapter"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

// SupervisorConfig configures a per-session supervisor.
type SupervisorConfig struct {
	StateDir   string          // root of persisted state (~/.local/share/rex)
	Store      *state.Store    // central store for state transitions
	Command    []string        // argv to spawn (command + args resolved from registry+model)
	CWD        string          // working directory for the child
	Adapter    adapter.Adapter // nil = no state classification (tests/echo tool)
	OutputSink func(b []byte)  // called with every chunk read from PTY; non-blocking
	IdleTick   time.Duration   // how often we sample idle for the adapter (default 200ms)
}

// Supervisor runs one PTY session to completion.
type Supervisor struct {
	cfg SupervisorConfig
}

// New constructs a Supervisor.
func New(cfg SupervisorConfig) *Supervisor {
	if cfg.IdleTick == 0 {
		cfg.IdleTick = 200 * time.Millisecond
	}
	return &Supervisor{cfg: cfg}
}

// Run starts the child process and blocks until it exits, ctx is canceled, or an error occurs.
func (s *Supervisor) Run(ctx context.Context, sess *state.Session) error {
	if len(s.cfg.Command) == 0 {
		return errors.New("supervisor: empty command")
	}

	cmd := exec.CommandContext(ctx, s.cfg.Command[0], s.cfg.Command[1:]...)
	if s.cfg.CWD != "" {
		cmd.Dir = s.cfg.CWD
	}
	f, err := pty.Start(cmd)
	if err != nil {
		_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
		return fmt.Errorf("pty start: %w", err)
	}
	defer f.Close()

	if err := s.cfg.Store.Transition(sess.ID, protocol.StateWorking); err != nil {
		return err
	}

	// Track the most recent output window and last-chunk time for adapter classification.
	// windowMu guards window and lastChunk accessed from both the reader goroutine and
	// the ticker select case.
	var windowMu sync.Mutex
	window := make([]byte, 0, 8192)
	lastChunk := time.Now()
	errc := make(chan error, 1)

	// Reader goroutine.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				if err := state.AppendTranscript(s.cfg.StateDir, sess.ID, chunk); err != nil {
					errc <- fmt.Errorf("persist transcript: %w", err)
					return
				}
				if s.cfg.OutputSink != nil {
					s.cfg.OutputSink(chunk)
				}
				windowMu.Lock()
				window = appendBounded(window, chunk, 8192)
				lastChunk = time.Now()
				line := lastNonEmptyLine(window)
				windowMu.Unlock()
				// Update last_line — last non-empty line in the window.
				if line != "" {
					_ = s.cfg.Store.UpdateLastLine(sess.ID, line)
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					errc <- nil
				} else {
					errc <- fmt.Errorf("pty read: %w", rerr)
				}
				return
			}
		}
	}()

	// Adapter classification ticker.
	ticker := time.NewTicker(s.cfg.IdleTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-errc
			_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
			return ctx.Err()
		case rerr := <-errc:
			// Child exited.
			waitErr := cmd.Wait()
			final := protocol.StateDone
			if waitErr != nil {
				if ee := new(exec.ExitError); errors.As(waitErr, &ee) {
					code := ee.ExitCode()
					sess.ExitCode = &code
					if code != 0 {
						final = protocol.StateFailed
					}
				} else if rerr != nil {
					final = protocol.StateFailed
				}
			}
			// Update in-memory state first so the persisted meta reflects the terminal state.
			sess.State = final
			sess.LastEventAt = time.Now().UTC()
			if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
				return fmt.Errorf("write final meta: %w", err)
			}
			// Now broadcast — subscribers can safely read meta.json after this fires.
			if err := s.cfg.Store.Transition(sess.ID, final); err != nil {
				return err
			}
			return nil
		case <-ticker.C:
			if s.cfg.Adapter == nil {
				continue
			}
			windowMu.Lock()
			idle := time.Since(lastChunk)
			windowSnap := append([]byte(nil), window...)
			windowMu.Unlock()
			next := s.cfg.Adapter.Detect(windowSnap, idle)
			if next == "" {
				continue
			}
			current := sess.State
			if next != current && next != protocol.StateWorking {
				_ = s.cfg.Store.Transition(sess.ID, next)
			}
		}
	}
}

func appendBounded(buf, b []byte, cap int) []byte {
	out := append(buf, b...)
	if len(out) > cap {
		out = out[len(out)-cap:]
	}
	return out
}

func lastNonEmptyLine(b []byte) string {
	// Walk backwards to find the last newline; return the trimmed segment after it.
	end := len(b)
	for end > 0 && (b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	start := end
	for start > 0 && b[start-1] != '\n' && b[start-1] != '\r' {
		start--
	}
	if start == end {
		return ""
	}
	return string(b[start:end])
}
