// Package pty supervises a single agent session: spawn, read, classify, persist.
package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/tristanbietsch/rex/internal/daemon/adapter"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/daemon/termtext"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// SupervisorConfig configures a per-session supervisor.
type SupervisorConfig struct {
	StateDir       string          // root of persisted state (~/.local/share/rex)
	Store          *state.Store    // central store for state transitions
	Command        []string        // argv to spawn (command + args resolved from registry+model)
	CWD            string          // working directory for the child
	Adapter        adapter.Adapter // nil = no state classification (tests/echo tool)
	OutputSink     func(b []byte)  // called with every chunk read from PTY; non-blocking
	SummaryRequest chan<- string   // optional: session IDs needing AI summary; nil disables
	InputCh        chan []byte     // optional: stdin bytes from clients
	// CompleteCh signals a clean shutdown — supervisor kills the child and
	// transitions to StateDone. Distinct from ctx cancel, which means StateFailed.
	// Buffered size 1; sender should use a non-blocking send.
	CompleteCh       chan struct{}
	IdleTick         time.Duration // how often we sample idle for the adapter (default 200ms)
	InitialCols      uint16        // initial PTY width (0 = default)
	InitialRows      uint16        // initial PTY height (0 = default)
	InitialPrompt    string        // optional: typed into the agent once its TUI settles
	PlainPaste       bool          // paste InitialPrompt without bracketed-paste markers (line REPLs)
	ReadySettle      time.Duration // output must be quiet this long before pasting InitialPrompt
	RegisterResize   func(resize func(cols, rows uint16) error)
	UnregisterResize func()
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
	cols := s.cfg.InitialCols
	rows := s.cfg.InitialRows
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 32
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		slog.Error("pty: start failed", "session", sess.ID, "argv", s.cfg.Command, "err", err)
		_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
		return fmt.Errorf("pty start: %w", err)
	}
	defer f.Close()
	slog.Info("pty: started", "session", sess.ID, "pid", cmd.Process.Pid, "cols", cols, "rows", rows, "argv", s.cfg.Command)

	// Virtual screen mirroring what the child has drawn. Full-screen TUIs
	// (Claude Code especially) repaint only changed cells with cursor jumps, so
	// readable text (last_line, summarizer input) must come from an emulated
	// screen, not from escape-stripped bytes.
	screen := vt10x.New(vt10x.WithSize(int(cols), int(rows)))

	// Expose a resize callback to subscribers (attach clients).
	if s.cfg.RegisterResize != nil {
		s.cfg.RegisterResize(func(c, r uint16) error {
			screen.Resize(int(c), int(r))
			return pty.Setsize(f, &pty.Winsize{Cols: c, Rows: r})
		})
		if s.cfg.UnregisterResize != nil {
			defer s.cfg.UnregisterResize()
		}
	}

	if s.cfg.InputCh != nil {
		go func() {
			for b := range s.cfg.InputCh {
				if _, err := f.Write(b); err != nil {
					return
				}
			}
		}()
	}

	if err := s.cfg.Store.Transition(sess.ID, protocol.StateWorking); err != nil {
		return err
	}

	transcript, err := state.OpenTranscript(s.cfg.StateDir, sess.ID)
	if err != nil {
		return err
	}
	defer transcript.Close()

	// Track the most recent output window and last-chunk time for adapter classification.
	// windowMu guards window and lastChunk accessed from both the reader goroutine and
	// the ticker select case.
	var windowMu sync.Mutex
	window := make([]byte, 0, 8192)
	lastChunk := time.Now()
	dirty := false
	screenDirty := false
	lastSummaryAt := time.Now()
	errc := make(chan error, 1)

	// If the caller provided an initial prompt (from the wizard's "describe the
	// task" step), type it into the agent once its TUI is parked at its input
	// field. Readiness is detected by the per-tool Adapter via IsReadyForInput
	// — each agent has its own signal (Claude/Codex/Gemini's input box ready,
	// Ollama's `>>>` prompt, etc.). Without an adapter we have no reliable
	// signal so the paste is skipped entirely.
	if s.cfg.InitialPrompt != "" {
		if s.cfg.Adapter == nil {
			slog.Warn("pty: initial_prompt set but no adapter; cannot detect readiness", "session", sess.ID)
		} else {
			go func(prompt string) {
				deadline := time.NewTimer(30 * time.Second)
				defer deadline.Stop()
				poll := time.NewTicker(200 * time.Millisecond)
				defer poll.Stop()
				for {
					select {
					case <-deadline.C:
						windowMu.Lock()
						tailLen := len(window)
						windowMu.Unlock()
						slog.Warn("pty: initial_prompt timed out waiting for ready", "session", sess.ID, "window_bytes", tailLen)
						return
					case <-ctx.Done():
						return
					case <-poll.C:
						windowMu.Lock()
						idle := time.Since(lastChunk)
						snap := append([]byte(nil), window...)
						windowMu.Unlock()
						if !s.cfg.Adapter.IsReadyForInput(snap, idle) {
							continue
						}
						// Some REPLs flash their prompt mid-redraw (ollama's spinner);
						// wait for a quiet beat so the paste lands on a settled screen.
						if idle < s.cfg.ReadySettle {
							continue
						}
						// Modern agent TUIs (Claude Code, Gemini CLI, Codex) detect
						// bracketed paste and silently drop bursts of raw text — the
						// only reliable way to fill their input fields is to wrap the
						// payload in DEC bracketed-paste markers (ESC[200~ … ESC[201~).
						// Then a SEPARATE write of \r commits the input. Sending text
						// and Enter in one chunk lets the TUI treat the whole thing as
						// a paste and never trigger submit.
						paste := []byte(prompt)
						if !s.cfg.PlainPaste {
							paste = append([]byte("\x1b[200~"), paste...)
							paste = append(paste, []byte("\x1b[201~")...)
						}
						if _, err := f.Write(paste); err != nil {
							slog.Warn("pty: initial_prompt paste failed", "session", sess.ID, "err", err)
							return
						}
						slog.Info("pty: initial_prompt pasted", "session", sess.ID, "bytes", len(paste), "plain", s.cfg.PlainPaste, "idle_ms", idle.Milliseconds())
						// Give the TUI a beat to commit the pasted text to its input
						// state before we hit Enter; without this Claude sometimes
						// fires Enter against an empty input box.
						select {
						case <-ctx.Done():
							return
						case <-time.After(120 * time.Millisecond):
						}
						if _, err := f.Write([]byte{'\r'}); err != nil {
							slog.Warn("pty: initial_prompt submit failed", "session", sess.ID, "err", err)
						} else {
							slog.Info("pty: initial_prompt submitted", "session", sess.ID)
						}
						return
					}
				}
			}(s.cfg.InitialPrompt)
		}
	}

	// tokenThreshold tracks the last token count at which we emitted a patch.
	// We broadcast every +500 tokens to avoid event spam.
	var lastBroadcastTokens int64

	// Reader goroutine.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				if err := transcript.Write(chunk); err != nil {
					errc <- fmt.Errorf("persist transcript: %w", err)
					return
				}
				if s.cfg.OutputSink != nil {
					// Subscribers may retain the slice; copy only when forwarding.
					out := append([]byte(nil), chunk...)
					s.cfg.OutputSink(out)
				}

				// Increment token counter and emit a patch every +500 tokens.
				// Tokens = OutputBytes / 4 (approximate heuristic).
				tokens, outputBytes, terr := s.cfg.Store.UpdateTokens(sess.ID, int64(n))
				if terr == nil {
					if tokens-lastBroadcastTokens >= 500 {
						lastBroadcastTokens = tokens
						s.cfg.Store.BroadcastTokenPatch(sess.ID, tokens, outputBytes)
						slog.Debug("pty: token patch broadcast", "session", sess.ID, "tokens", tokens, "output_bytes", outputBytes)
					}
				}
				// Cursor-blink and other escape-only chunks (DECTCEM toggle,
				// SGR resets) must NOT reset the idle timer — otherwise the
				// heuristic adapter never sees idle ≥ idle_ms and codex/claude
				// stay pinned at "working" forever while sitting at a prompt.
				visible := hasVisibleText(chunk)
				_, _ = screen.Write(chunk)
				windowMu.Lock()
				window = appendBounded(window, chunk, 8192)
				if visible {
					lastChunk = time.Now()
					dirty = true
					screenDirty = true
				}
				windowMu.Unlock()
				if !visible {
					slog.Debug("pty: ignored escape-only chunk for idle", "session", sess.ID, "bytes", len(chunk))
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
			slog.Info("pty: ctx canceled, killing child", "session", sess.ID, "pid", cmd.Process.Pid)
			_ = cmd.Process.Kill()
			<-errc
			_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
			return ctx.Err()
		case <-s.cfg.CompleteCh:
			slog.Info("pty: complete signal received, killing child cleanly",
				"session", sess.ID, "pid", cmd.Process.Pid)
			_ = cmd.Process.Kill()
			<-errc         // drain the reader goroutine
			_ = cmd.Wait() // reap the child; mirrors the natural-exit branch
			if err := s.cfg.Store.Transition(sess.ID, protocol.StateDone); err != nil {
				return err
			}
			sess.LastEventAt = time.Now().UTC()
			if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
				return fmt.Errorf("write final meta: %w", err)
			}
			return nil
		case rerr := <-errc:
			// Child exited.
			s.refreshScreenText(sess.ID, screen)
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
			slog.Info("pty: child exited", "session", sess.ID, "final_state", final, "wait_err", waitErr, "read_err", rerr)
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
			// Adapter classification (existing).
			if s.cfg.Adapter != nil {
				windowMu.Lock()
				idle := time.Since(lastChunk)
				windowSnap := append([]byte(nil), window...)
				windowMu.Unlock()
				next := s.cfg.Adapter.Detect(windowSnap, idle)
				if next != "" {
					if current, ok := s.cfg.Store.CurrentState(sess.ID); ok && next != current {
						_ = s.cfg.Store.Transition(sess.ID, next)
					}
				}
			}

			// Readable screen → last_line + summarizer input. Sampled on the
			// tick (not per chunk) so spinner repaints don't flood patches.
			windowMu.Lock()
			refresh := screenDirty
			screenDirty = false
			windowMu.Unlock()
			if refresh {
				s.refreshScreenText(sess.ID, screen)
			}

			// Summary trigger (additive — independent of adapter).
			if s.cfg.SummaryRequest != nil {
				now := time.Now()
				windowMu.Lock()
				emit := shouldEmitSummary(dirty, lastChunk, lastSummaryAt, now)
				windowMu.Unlock()
				if emit {
					select {
					case s.cfg.SummaryRequest <- sess.ID:
						windowMu.Lock()
						dirty = false
						lastSummaryAt = now
						windowMu.Unlock()
						slog.Debug("pty: summary signal sent", "session", sess.ID)
					default:
						slog.Debug("pty: summary worker busy, will retry next tick", "session", sess.ID)
					}
				}
			}
		}
	}
}

// summaryIdleThreshold is the quiet-period after a burst that marks the
// "natural beat" for re-summarizing. 500ms matches a typical agent pause
// between operations and never races the 200ms IdleTick.
const summaryIdleThreshold = 500 * time.Millisecond

// summaryCeiling is the maximum interval between summaries for a never-idle
// (continuously chatty) session.
const summaryCeiling = 15 * time.Second

// shouldEmitSummary is the pure predicate used in the ticker branch.
func shouldEmitSummary(dirty bool, lastChunk, lastSubmittedAt, now time.Time) bool {
	if !dirty {
		return false
	}
	if now.Sub(lastChunk) >= summaryIdleThreshold {
		return true
	}
	if now.Sub(lastSubmittedAt) >= summaryCeiling {
		return true
	}
	return false
}

func appendBounded(buf, b []byte, cap int) []byte {
	out := append(buf, b...)
	if len(out) > cap {
		out = out[len(out)-cap:]
	}
	return out
}

// screenTailBytes caps the screen text kept for the summarizer.
const screenTailBytes = 2048

// refreshScreenText derives last_line and the summarizer's screen text from
// the virtual screen, dropping UI chrome (spinners, borders, key hints).
func (s *Supervisor) refreshScreenText(id string, screen vt10x.Terminal) {
	lines := termtext.Lines([]byte(screen.String()))
	if len(lines) == 0 {
		return
	}
	s.cfg.Store.SetScreen(id, termtext.Tail([]byte(strings.Join(lines, "\n")), screenTailBytes))
	last := sanitizeForDisplay(lines[len(lines)-1])
	if last == "" {
		return
	}
	if err := s.cfg.Store.UpdateLastLine(id, last); err != nil {
		slog.Warn("pty: update last_line failed", "session", id, "err", err)
	}
}
