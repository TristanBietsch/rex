// Package pty supervises a single agent session: spawn, read, classify, persist.
package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	StateDir         string          // root of persisted state (~/.local/share/rex)
	Store            *state.Store    // central store for state transitions
	Command          []string        // argv to spawn (command + args resolved from registry+model)
	CWD              string          // working directory for the child
	Adapter          adapter.Adapter // nil = no state classification (tests/echo tool)
	OutputSink       func(b []byte)  // called with every chunk read from PTY; non-blocking
	SummaryRequest   chan<- string   // optional: session IDs needing AI summary; nil disables
	InputCh          chan []byte     // optional: stdin bytes from clients
	IdleTick         time.Duration   // how often we sample idle for the adapter (default 200ms)
	InitialCols      uint16          // initial PTY width (0 = default)
	InitialRows      uint16          // initial PTY height (0 = default)
	InitialPrompt    string          // optional: typed into the agent once its TUI settles
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

	// Expose a resize callback to subscribers (attach clients).
	if s.cfg.RegisterResize != nil {
		s.cfg.RegisterResize(func(c, r uint16) error {
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

	// Track the most recent output window and last-chunk time for adapter classification.
	// windowMu guards window and lastChunk accessed from both the reader goroutine and
	// the ticker select case.
	var windowMu sync.Mutex
	window := make([]byte, 0, 8192)
	lastChunk := time.Now()
	dirty := false
	lastSummaryAt := time.Now()
	errc := make(chan error, 1)

	// If the caller provided an initial prompt (from the wizard's "describe the
	// task" step), type it into the agent once its TUI settles. We detect
	// "settled" by waiting for at least one chunk of output and then ~800ms of
	// quiet — that's "agent finished rendering and is parked at its prompt."
	if s.cfg.InitialPrompt != "" {
		go func(prompt string) {
			deadline := time.NewTimer(30 * time.Second)
			defer deadline.Stop()
			poll := time.NewTicker(200 * time.Millisecond)
			defer poll.Stop()
			for {
				select {
				case <-deadline.C:
					slog.Warn("pty: initial_prompt timed out waiting for ready", "session", sess.ID)
					return
				case <-ctx.Done():
					return
				case <-poll.C:
					windowMu.Lock()
					ready := len(window) > 0 && time.Since(lastChunk) > 800*time.Millisecond
					windowMu.Unlock()
					if !ready {
						continue
					}
					// Modern agent TUIs (Claude Code, Gemini CLI, Codex) detect
					// bracketed paste and silently drop bursts of raw text — the
					// only reliable way to fill their input fields is to wrap the
					// payload in DEC bracketed-paste markers (ESC[200~ … ESC[201~).
					// Then a SEPARATE write of \r commits the input. Sending text
					// and Enter in one chunk lets the TUI treat the whole thing as
					// a paste and never trigger submit.
					paste := append([]byte("\x1b[200~"), []byte(prompt)...)
					paste = append(paste, []byte("\x1b[201~")...)
					if _, err := f.Write(paste); err != nil {
						slog.Warn("pty: initial_prompt paste failed", "session", sess.ID, "err", err)
						return
					}
					slog.Info("pty: initial_prompt pasted", "session", sess.ID, "bytes", len(paste))
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

	// tokenThreshold tracks the last token count at which we emitted a patch.
	// We broadcast every +500 tokens to avoid event spam.
	var lastBroadcastTokens int64

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
				windowMu.Lock()
				window = appendBounded(window, chunk, 8192)
				if visible {
					lastChunk = time.Now()
					dirty = true
				}
				line := lastNonEmptyLine(window)
				windowMu.Unlock()
				if !visible {
					slog.Debug("pty: ignored escape-only chunk for idle", "session", sess.ID, "bytes", len(chunk))
				}
				// Update last_line — sanitized so cursor/erase/color escapes
				// can't escape the TUI's row cell and corrupt the screen.
				if line != "" {
					clean := sanitizeForDisplay(line)
					if clean == "" {
						slog.Debug("pty: last_line all-control, skipping update", "session", sess.ID, "raw_len", len(line))
					} else {
						if clean != line {
							slog.Debug("pty: sanitized last_line", "session", sess.ID, "raw_len", len(line), "clean_len", len(clean))
						}
						if err := s.cfg.Store.UpdateLastLine(sess.ID, clean); err != nil {
							slog.Warn("pty: update last_line failed", "session", sess.ID, "err", err)
						}
					}
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
					current := sess.State
					if next != current && next != protocol.StateWorking {
						_ = s.cfg.Store.Transition(sess.ID, next)
					}
				}
			}

			// Summary trigger (additive — independent of adapter).
			if s.cfg.SummaryRequest != nil {
				windowMu.Lock()
				emit := shouldEmitSummary(dirty, lastChunk, lastSummaryAt, time.Now())
				windowMu.Unlock()
				if emit {
					select {
					case s.cfg.SummaryRequest <- sess.ID:
						windowMu.Lock()
						dirty = false
						lastSummaryAt = time.Now()
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

func lastNonEmptyLine(b []byte) string {
	// Walk backwards across lines. Skip lines that are mostly spinner glyphs
	// (Braille block U+2800–U+28FF, used by ollama/gemini for loading spinners) —
	// those would otherwise clobber the board's description column with animation
	// frames. Falls back to the last line if every recent line is spinner-y.
	end := len(b)
	var fallback string
	for end > 0 {
		for end > 0 && (b[end-1] == '\n' || b[end-1] == '\r') {
			end--
		}
		start := end
		for start > 0 && b[start-1] != '\n' && b[start-1] != '\r' {
			start--
		}
		if start == end {
			break
		}
		line := string(b[start:end])
		if fallback == "" {
			fallback = line
		}
		if !isSpinnerLine(line) {
			return line
		}
		end = start
	}
	return fallback
}

// isSpinnerLine reports whether s is overwhelmingly Braille-block glyphs
// (the animation chars used by ollama/gemini progress spinners). We accept up
// to a handful of stray spaces / brackets / dots so partial frames still count.
func isSpinnerLine(s string) bool {
	if s == "" {
		return false
	}
	var spinner, other int
	for _, r := range s {
		switch {
		case r >= 0x2800 && r <= 0x28FF:
			spinner++
		case r == ' ' || r == '.' || r == '·' || r == '…':
			// neutral — don't count either way
		default:
			other++
		}
	}
	return spinner >= 3 && spinner > other*3
}
