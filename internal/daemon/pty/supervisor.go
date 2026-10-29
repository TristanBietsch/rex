// Package pty supervises a single agent session: spawn, read, classify, persist.
package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hinshun/vt10x"
	"github.com/tristanbietsch/rex/internal/daemon/adapter"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/daemon/termtext"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ErrSessionDeleted is the context cause the server uses when it tears a
// session down for deletion (user Delete or retire sweep). The supervisor
// then skips the StateFailed transition: the session is going away, and a
// transient "failed" patch would mislead clients and Lua hooks.
var ErrSessionDeleted = errors.New("session deleted")

// defaultStopDrainTimeout bounds how long Run waits for the PTY reader after
// killing the child. A grandchild that inherited the PTY can keep it open
// past the kill; without a bound, Delete (and the retire sweep) would hang.
const defaultStopDrainTimeout = 5 * time.Second

// SupervisorConfig configures a per-session supervisor.
type SupervisorConfig struct {
	StateDir       string          // root of persisted state (~/.local/share/rex)
	Store          *state.Store    // central store for state transitions
	Command        []string        // argv to spawn (command + args resolved from registry+model)
	CWD            string          // working directory for the child
	Env            []string        // extra KEY=VALUE entries added to the child's environment
	Adapter        adapter.Adapter // nil = no state classification (tests/echo tool)
	OutputSink     func(b []byte)  // called with every chunk read from PTY; must not block (it runs on the reader goroutine)
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
	// RegisterModePreamble exposes the child's current terminal modes as an
	// escape-sequence preamble (see ModePreamble) to attach clients.
	RegisterModePreamble   func(preamble func() []byte)
	UnregisterModePreamble func()
	// StopDrainTimeout bounds the reader drain after a kill (0 = 5s).
	StopDrainTimeout time.Duration
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
	if cfg.StopDrainTimeout == 0 {
		cfg.StopDrainTimeout = defaultStopDrainTimeout
	}
	return &Supervisor{cfg: cfg}
}

// Run starts the child process and blocks until it exits, ctx is canceled, or an error occurs.
func (s *Supervisor) Run(ctx context.Context, sess *state.Session) error {
	if len(s.cfg.Command) == 0 {
		return errors.New("supervisor: empty command")
	}

	// Not CommandContext: on cancel it kills only the direct child, and the
	// reader's EOF then races the ctx.Done branch below. Every stop goes
	// through killAndReap, which kills the whole tree.
	cmd := exec.Command(s.cfg.Command[0], s.cfg.Command[1:]...)
	if s.cfg.CWD != "" {
		cmd.Dir = s.cfg.CWD
	}
	if len(s.cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), s.cfg.Env...)
	}
	cols := s.cfg.InitialCols
	rows := s.cfg.InitialRows
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 32
	}
	// Log the command's shape, never argv itself: it can carry the user's
	// prompt and hook settings JSON.
	tool, argc, promptBytes := filepath.Base(s.cfg.Command[0]), len(s.cfg.Command)-1, len(s.cfg.InitialPrompt)
	f, err := startConsole(cmd, cols, rows)
	if err != nil {
		slog.Error("pty: start failed", "session", sess.ID, "tool", tool, "argc", argc, "err", err)
		_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
		return fmt.Errorf("pty start: %w", err)
	}
	defer f.Close()
	slog.Info("pty: started", "session", sess.ID, "pid", cmd.Process.Pid, "cols", cols, "rows", rows,
		"tool", tool, "argc", argc, "initial_prompt_bytes", promptBytes)

	// Virtual screen mirroring what the child has drawn. Full-screen TUIs
	// (Claude Code especially) repaint only changed cells with cursor jumps, so
	// readable text (last_line, summarizer input) must come from an emulated
	// screen, not from escape-stripped bytes.
	screen := vt10x.New(vt10x.WithSize(int(cols), int(rows)))

	// Expose a resize callback to subscribers (attach clients).
	if s.cfg.RegisterResize != nil {
		s.cfg.RegisterResize(func(c, r uint16) error {
			screen.Resize(int(c), int(r))
			return f.Resize(c, r)
		})
		if s.cfg.UnregisterResize != nil {
			defer s.cfg.UnregisterResize()
		}
	}

	if s.cfg.RegisterModePreamble != nil {
		s.cfg.RegisterModePreamble(func() []byte {
			screen.Lock()
			defer screen.Unlock()
			return ModePreamble(screen.Mode())
		})
		if s.cfg.UnregisterModePreamble != nil {
			defer s.cfg.UnregisterModePreamble()
		}
	}

	if s.cfg.InputCh != nil {
		// InputCh is never closed (the server only unregisters it), so the
		// forwarder must stop with Run or it leaks one goroutine per session.
		inputDone := make(chan struct{})
		defer close(inputDone)
		go func() {
			for {
				select {
				case <-inputDone:
					return
				case b := <-s.cfg.InputCh:
					if _, err := f.Write(b); err != nil {
						slog.Debug("pty: input write failed; forwarder exiting", "session", sess.ID, "err", err)
						return
					}
				}
			}
		}()
	}

	if err := s.cfg.Store.Transition(sess.ID, protocol.StateWorking); err != nil {
		s.killAndReap(sess.ID, cmd, f, nil)
		_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
		return err
	}
	// Persist meta for live sessions too, so a daemon crash reloads them as
	// crashed instead of silently dropping them.
	s.persistMeta(sess)

	transcript, err := state.OpenTranscript(s.cfg.StateDir, sess.ID)
	if err != nil {
		// Nothing would read the child's output; stop it rather than leave it
		// running unsupervised with the session stuck in working.
		s.killAndReap(sess.ID, cmd, f, nil)
		sess.SetExit(protocol.StateFailed, nil)
		s.persistMeta(sess)
		_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
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
						// a paste and never trigger submit. The prompt itself must not
						// carry escapes or controls (see sanitizePaste).
						paste := []byte(sanitizePaste(prompt))
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
		// partial holds a UTF-8 sequence split across reads. vt10x drops
		// the bytes of a rune cut off at the end of a Write, so they wait
		// here for the rest of the rune.
		var partial []byte
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
				text := chunk
				if len(partial) > 0 {
					text = slices.Concat(partial, chunk)
				}
				keep := incompleteRuneTail(text)
				_, _ = screen.Write(text[:len(text)-keep])
				partial = append([]byte(nil), text[len(text)-keep:]...)
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
			cause := context.Cause(ctx)
			slog.Info("pty: ctx canceled, killing child", "session", sess.ID, "pid", cmd.Process.Pid, "cause", cause)
			s.killAndReap(sess.ID, cmd, f, errc)
			if errors.Is(cause, ErrSessionDeleted) {
				slog.Info("pty: session deleted; skipping failed transition", "session", sess.ID)
				return ctx.Err()
			}
			// Persist the final state as well: meta.json still says working,
			// so a daemon shutdown would reload the session as crashed.
			sess.SetExit(protocol.StateFailed, nil)
			s.persistMeta(sess)
			_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
			return ctx.Err()
		case <-s.cfg.CompleteCh:
			slog.Info("pty: complete signal received, killing child cleanly",
				"session", sess.ID, "pid", cmd.Process.Pid)
			s.killAndReap(sess.ID, cmd, f, errc)
			if err := s.cfg.Store.Transition(sess.ID, protocol.StateDone); err != nil {
				return err
			}
			if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
				return fmt.Errorf("write final meta: %w", err)
			}
			return nil
		case rerr := <-errc:
			// Child exited.
			s.refreshScreenText(sess.ID, screen)
			// Background jobs and MCP servers the agent left behind are still
			// in its process group. Kill them before the reap: until then the
			// zombie holds the group id, so the signal can't hit a reused pid.
			if err := f.KillTree(); err != nil {
				slog.Warn("pty: kill leftover process tree failed", "session", sess.ID, "pid", cmd.Process.Pid, "err", err)
			}
			waitErr := f.Wait()
			final := protocol.StateDone
			var exitCode *int
			if waitErr != nil {
				if ee := new(exec.ExitError); errors.As(waitErr, &ee) {
					code := ee.ExitCode()
					exitCode = &code
					if code != 0 {
						final = protocol.StateFailed
					}
				} else if rerr != nil {
					final = protocol.StateFailed
				}
			}
			slog.Info("pty: child exited", "session", sess.ID, "final_state", final, "wait_err", waitErr, "read_err", rerr)
			// Update in-memory state first (under the session lock) so the
			// persisted meta reflects the terminal state.
			sess.SetExit(final, exitCode)
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
				current, ok := s.cfg.Store.CurrentState(sess.ID)
				// done is terminal: once the adapter reports it (done_regex),
				// later frames can't pull the session back to working.
				if next != "" && ok && next != current && current != protocol.StateDone {
					slog.Info("pty: state transition", "session", sess.ID, "from", current, "to", next, "idle_ms", idle.Milliseconds())
					_ = s.cfg.Store.Transition(sess.ID, next)
					s.persistMeta(sess)
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

// incompleteRuneTail returns how many trailing bytes of b start a UTF-8
// sequence that a later read will complete (0 when b ends on a rune
// boundary). Invalid bytes count as complete, so nothing is held forever.
func incompleteRuneTail(b []byte) int {
	for i := len(b) - 1; i >= 0 && i > len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return 0
			}
			return len(b) - i
		}
	}
	return 0
}

func appendBounded(buf, b []byte, cap int) []byte {
	buf = append(buf, b...)
	if len(buf) > cap {
		buf = buf[len(buf)-cap:]
	}
	return buf
}

// killAndReap kills the child's whole process tree (console.KillTree) —
// agents spawn MCP servers and tool shells that inherit the PTY and would
// otherwise keep the reader from seeing EOF. The reader drain and the reap are
// both bounded by StopDrainTimeout; on timeout Run returns anyway and the
// deferred PTY close unblocks the reader. A nil errc (no reader started yet)
// skips the drain.
func (s *Supervisor) killAndReap(id string, cmd *exec.Cmd, f console, errc <-chan error) {
	pid := cmd.Process.Pid
	if err := f.KillTree(); err != nil {
		slog.Warn("pty: kill process tree failed; killing pid", "session", id, "pid", pid, "err", err)
		_ = cmd.Process.Kill()
	}
	timeout := time.NewTimer(s.cfg.StopDrainTimeout)
	defer timeout.Stop()
	if errc != nil {
		select {
		case <-errc:
		case <-timeout.C:
			slog.Error("pty: stop drain timed out; PTY still held open", "session", id, "pid", pid, "timeout", s.cfg.StopDrainTimeout)
			return
		}
	}
	waited := make(chan struct{})
	go func() {
		_ = f.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-timeout.C:
		slog.Error("pty: reap timed out", "session", id, "pid", pid)
	}
}

// persistMeta writes meta.json for a live session; failures are logged only.
func (s *Supervisor) persistMeta(sess *state.Session) {
	if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
		slog.Warn("pty: write meta failed", "session", sess.ID, "err", err)
	}
}

// screenTailBytes caps the screen text kept for the summarizer.
const screenTailBytes = 2048

// refreshScreenText derives last_line and the summarizer's screen text from
// the virtual screen, dropping UI chrome (spinners, borders, key hints).
func (s *Supervisor) refreshScreenText(id string, screen vt10x.Terminal) {
	lines := termtext.ScreenLines(screen.String())
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
