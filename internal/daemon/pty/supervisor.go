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
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

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
	DropEnv        []string        // inherited variable names removed from the child's environment
	Adapter        adapter.Adapter // nil = no state classification (tests/echo tool)
	OutputSink     func(b []byte)  // called with every chunk read from PTY; non-blocking
	SummaryRequest chan<- string   // optional: session IDs needing AI summary; nil disables
	InputCh        chan []byte     // optional: stdin bytes from clients
	// RegisterWriter hands the server the session's UnitWriter once the
	// writer runs, UnregisterWriter takes it back at exit. Optional.
	RegisterWriter   func(w UnitWriter)
	UnregisterWriter func()
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
	// RegisterAttach exposes a hook for attach clients: it runs fn with the
	// session's output held, so no PTY chunk is persisted, passed to
	// OutputSink or applied to the screen while fn runs. fn's repaint
	// returns the current screen as escape sequences (see Repaint). A
	// subscriber that snapshots (transcript tail, repaint) and registers
	// its output sink inside fn sees each chunk exactly once: in the
	// snapshot or live. fn must not block on the session's output.
	RegisterAttach   func(attach func(fn func(repaint func() []byte)))
	UnregisterAttach func()
	// StopDrainTimeout bounds the reader drain after a kill (0 = 5s).
	StopDrainTimeout time.Duration
	// TrustPrompt, when set, accepts the agent's folder-trust dialog (the
	// session's folder is under auto_trust_dirs). nil leaves it to the user.
	TrustPrompt *TrustAnswer
	// ResumeCapture, when set, reads the agent's conversation id from its
	// last output when the child exits: the last match's group 1 on the
	// normalized screen text, kept only if it is a valid resume id.
	ResumeCapture *regexp.Regexp
	// NoResumeIDs turns off every resume-id source (the adapter's hook
	// reports and ResumeCapture): the feature flag "resume" is off.
	NoResumeIDs bool
}

// transcriptWriter persists the raw PTY output (*state.TranscriptFile in
// production).
type transcriptWriter interface {
	Write(b []byte) error
	Close() error
}

// Supervisor runs one PTY session to completion.
type Supervisor struct {
	cfg SupervisorConfig
	// openTranscript opens the session's transcript. Tests swap it to inject
	// open and write failures (a full disk).
	openTranscript func(root, id string) (transcriptWriter, error)
}

// New constructs a Supervisor.
func New(cfg SupervisorConfig) *Supervisor {
	if cfg.IdleTick == 0 {
		cfg.IdleTick = 200 * time.Millisecond
	}
	if cfg.StopDrainTimeout == 0 {
		cfg.StopDrainTimeout = defaultStopDrainTimeout
	}
	return &Supervisor{cfg: cfg, openTranscript: openStateTranscript}
}

// openStateTranscript adapts state.OpenTranscript to transcriptWriter.
func openStateTranscript(root, id string) (transcriptWriter, error) {
	tf, err := state.OpenTranscript(root, id)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	return tf, nil
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
	if len(s.cfg.Env) > 0 || len(s.cfg.DropEnv) > 0 {
		cmd.Env = append(envWithout(os.Environ(), s.cfg.DropEnv), s.cfg.Env...)
	}
	cols := s.cfg.InitialCols
	rows := s.cfg.InitialRows
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 32
	}
	f, err := startConsole(cmd, cols, rows)
	if err != nil {
		slog.Error("pty: start failed", append([]any{"session", sess.ID, "err", err}, argvLogAttrs(sess, s.cfg.Command)...)...)
		_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
		return fmt.Errorf("pty start: %w", err)
	}
	defer f.Close()
	// Everything written to the child goes through pw, so a child that stops
	// reading its stdin stalls only the writer goroutine (it ends with the PTY).
	writerDone := make(chan struct{})
	defer close(writerDone)
	pw := newPTYWriter(f, writerDone, func(err error) {
		slog.Debug("pty: write to child failed; writer exiting", "session", sess.ID, "err", err)
	})
	if s.cfg.RegisterWriter != nil {
		s.cfg.RegisterWriter(unitWriter(pw))
		if s.cfg.UnregisterWriter != nil {
			defer s.cfg.UnregisterWriter()
		}
	}
	slog.Info("pty: started", append([]any{"session", sess.ID, "pid", cmd.Process.Pid, "cols", cols, "rows", rows},
		argvLogAttrs(sess, s.cfg.Command)...)...)

	// Until the main loop owns the child (owned is set just before the reader
	// starts), an early return must not orphan it: kill and reap it on the
	// way out. Registered after f.Close so it runs before the PTY closes.
	owned := false
	defer func() {
		if !owned {
			_ = s.killAndReapNow(sess.ID, cmd, f)
		}
	}()

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

	// outMu makes each chunk's transcript write, fan-out and screen update
	// one step as far as attach snapshots are concerned (RegisterAttach).
	var outMu sync.Mutex
	if s.cfg.RegisterAttach != nil {
		s.cfg.RegisterAttach(func(fn func(repaint func() []byte)) {
			outMu.Lock()
			defer outMu.Unlock()
			fn(func() []byte {
				screen.Lock()
				defer screen.Unlock()
				return Repaint(screen)
			})
		})
		if s.cfg.UnregisterAttach != nil {
			defer s.cfg.UnregisterAttach()
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
					if !pw.Send(ctx, writeUnit{parts: [][]byte{b}}) {
						return
					}
				}
			}
		}()
	}

	if err := s.cfg.Store.Transition(sess.ID, protocol.StateWorking); err != nil {
		return err
	}
	// Persist meta for live sessions too, so a daemon crash reloads them as
	// crashed instead of silently dropping them.
	s.persistMeta(sess)

	transcript, err := s.openTranscript(s.cfg.StateDir, sess.ID)
	if err != nil {
		return err
	}
	defer transcript.Close()
	tlog := newPausingTranscript(transcript, sess.ID, nil)

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
						// One unit: the paste, a beat for the TUI to commit it to its
						// input state (without it Claude sometimes fires Enter against
						// an empty box), then Enter, with no other input in between.
						if !pw.Send(ctx, writeUnit{parts: [][]byte{paste, {'\r'}}, gap: 120 * time.Millisecond}) {
							slog.Warn("pty: initial_prompt not sent", "session", sess.ID)
							return
						}
						slog.Info("pty: initial_prompt queued", "session", sess.ID, "bytes", len(paste), "plain", s.cfg.PlainPaste, "idle_ms", idle.Milliseconds())
						return
					}
				}
			}(s.cfg.InitialPrompt)
		}
	}

	// tokenThreshold tracks the last token count at which we emitted a patch.
	// We broadcast every +500 tokens to avoid event spam.
	var lastBroadcastTokens int64

	// From here every exit of the main loop kills or reaps the child.
	owned = true

	// observer is asserted once; nil unless the adapter reads the raw stream.
	observer, _ := s.cfg.Adapter.(adapter.OutputObserver)
	responder, _ := s.cfg.Adapter.(adapter.OutputResponder)
	// A reply the writer could not take (it was writing a unit) stays pending
	// and is tried again when the writer is idle, so an agent that asks the
	// terminal once is still answered. The adapter's token bucket was spent
	// when the query was seen; a retry never spends another.
	var replyMu sync.Mutex
	var pendingReply []byte
	retryReply := func() {
		replyMu.Lock()
		defer replyMu.Unlock()
		if pendingReply != nil && pw.TrySendReply(pendingReply) {
			pendingReply = nil
		}
	}

	// Reader goroutine. It sends exactly one value on errc and returns: nil
	// when the child hung up the PTY, otherwise the read error, after which
	// the child is still running. Transcript write errors pause the
	// transcript (pausingTranscript) and never end the reader.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				outMu.Lock()
				tlog.Write(chunk) // a failing disk pauses it; never ends the session
				if s.cfg.OutputSink != nil {
					// Subscribers may retain the slice; copy only when forwarding.
					out := append([]byte(nil), chunk...)
					s.cfg.OutputSink(out)
				}
				_, _ = screen.Write(chunk)
				outMu.Unlock()
				if observer != nil {
					observer.Observe(chunk)
					// An agent that asks the terminal a question gets its answer
					// straight away (OSC 7501 support query).
					// Queued behind nothing or dropped: never blocks this reader.
					if responder != nil {
						if reply := responder.TakeReply(); len(reply) > 0 {
							replyMu.Lock()
							pendingReply = reply
							replyMu.Unlock()
						}
						retryReply()
					}
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
					screenDirty = true
				}
				windowMu.Unlock()
				if !visible {
					slog.Debug("pty: ignored escape-only chunk for idle", "session", sess.ID, "bytes", len(chunk))
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) || isHangup(rerr) {
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
	trust := newTrustAnswerer(s.cfg.TrustPrompt, time.Now())
	agents, _ := s.cfg.Adapter.(adapter.SubagentCounter) // nil unless the agent reports subagents
	// clearAgents zeroes the count once the agent is gone, and its
	// subagents with it. The exit paths call it before the final meta.json
	// write, so the file never keeps a count; the defer covers the rest.
	waits, _ := s.cfg.Adapter.(adapter.WaitReporter) // nil unless the agent reports tool waits
	clearAgents := func() {
		if agents != nil {
			_ = s.cfg.Store.SetSubagents(sess.ID, 0)
		}
		if waits != nil {
			_ = s.cfg.Store.SetWaiting(sess.ID, "", time.Time{})
		}
	}
	defer clearAgents()
	// reasons is asserted once too; nil unless the agent reports why it
	// waits. Cleared with the subagents on every exit path.
	reasons, _ := s.cfg.Adapter.(adapter.WaitReasoner)
	noticer, _ := s.cfg.Adapter.(adapter.Noticer)
	var lastNotice string
	clearReason := func() {
		if reasons != nil {
			_ = s.cfg.Store.SetWaitReason(sess.ID, "")
		}
	}
	defer clearReason()
	defer ticker.Stop()
	// ids is asserted once, like agents; nil unless the agent reports its
	// conversation id while it runs.
	var ids adapter.ResumeIDReporter
	if !s.cfg.NoResumeIDs {
		ids, _ = s.cfg.Adapter.(adapter.ResumeIDReporter)
	}
	// captureResumeID reads the id from the last output (ResumeCapture) and
	// stores it, without broadcasting. It reports whether the id changed; the
	// caller writes meta.json and then sends the resume_id patch.
	captureResumeID := func() bool {
		if s.cfg.ResumeCapture == nil || s.cfg.NoResumeIDs {
			return false
		}
		windowMu.Lock()
		text := termtext.Normalize(window)
		windowMu.Unlock()
		matches := s.cfg.ResumeCapture.FindAllStringSubmatch(text, -1)
		for i := len(matches) - 1; i >= 0; i-- {
			if len(matches[i]) < 2 {
				continue
			}
			if _, ok := protocol.NormalizeResumeID(matches[i][1]); !ok {
				continue
			}
			changed, err := s.cfg.Store.SetResumeID(sess.ID, matches[i][1])
			if err != nil {
				slog.Warn("pty: store captured resume id failed", "session", sess.ID, "err", err)
				return false
			}
			return changed
		}
		return false
	}
	// announceResumeID sends the resume_id patch for a captured id.
	announceResumeID := func() {
		s.cfg.Store.BroadcastPatch(sess.ID, map[string]any{"resume_id": sess.Summary().ResumeID})
	}

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
			_ = s.cfg.Store.Transition(sess.ID, protocol.StateFailed)
			return ctx.Err()
		case <-s.cfg.CompleteCh:
			slog.Info("pty: complete signal received, killing child cleanly",
				"session", sess.ID, "pid", cmd.Process.Pid)
			s.killAndReap(sess.ID, cmd, f, errc)
			// Same order as the child-exit path below.
			s.refreshScreenText(sess.ID, screen)
			captured := captureResumeID()
			clearAgents()
			clearReason()
			sess.SetExit(protocol.StateDone, nil)
			if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
				return fmt.Errorf("write final meta: %w", err)
			}
			if err := s.cfg.Store.Transition(sess.ID, protocol.StateDone); err != nil {
				return err
			}
			if captured {
				announceResumeID()
			}
			return nil
		case rerr := <-errc:
			// The reader stopped: the child exited, or reading
			// failed and the child is still running.
			s.refreshScreenText(sess.ID, screen)
			final, exitCode, waitErr := s.collectExit(sess.ID, cmd, f, rerr)
			slog.Info("pty: child exited", "session", sess.ID, "final_state", final, "wait_err", waitErr, "read_err", rerr)
			captured := captureResumeID()
			// Update in-memory state first (under the session lock) so the
			// persisted meta reflects the terminal state.
			sess.SetExit(final, exitCode)
			clearAgents()
			clearReason()
			if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
				return fmt.Errorf("write final meta: %w", err)
			}
			// Now broadcast — subscribers can safely read meta.json after this fires.
			if err := s.cfg.Store.Transition(sess.ID, final); err != nil {
				return err
			}
			if captured {
				announceResumeID()
			}
			if rerr != nil {
				return fmt.Errorf("pty reader: %w", rerr)
			}
			return nil
		case <-ticker.C:
			retryReply() // a reply that waited for the writer
			// Readable screen → last_line + summarizer input. Sampled on the
			// tick (not per chunk) so spinner repaints don't flood patches.
			// It comes before the classification below, so a subscriber that
			// reads last_line or the screen when a turn ends (the state
			// change or the wait reason) sees the agent's final output, not
			// the previous tick's.
			windowMu.Lock()
			refresh := screenDirty
			screenDirty = false
			windowMu.Unlock()
			// A notice appearing or going away changes the last line even
			// though the screen did not (a static login screen).
			if noticer != nil {
				if n := noticer.Notice(); n != lastNotice {
					lastNotice = n
					refresh = true
				}
			}
			if refresh {
				s.refreshScreenText(sess.ID, screen)
			}

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
			if agents != nil {
				_ = s.cfg.Store.SetSubagents(sess.ID, agents.Subagents()) // broadcasts only a change
			}
			if reasons != nil {
				_ = s.cfg.Store.SetWaitReason(sess.ID, reasons.WaitReason()) // broadcasts only a change
			}
			if waits != nil {
				kind, since := waits.Waiting()
				if err := s.cfg.Store.SetWaiting(sess.ID, kind, since); err != nil { // broadcasts only a change
					slog.Warn("pty: store reported wait failed", "session", sess.ID, "err", err)
				}
			}
			if ids != nil {
				if rid := ids.ResumeID(); rid != "" {
					changed, err := s.cfg.Store.SetResumeID(sess.ID, rid)
					if err != nil {
						slog.Warn("pty: store reported resume id failed", "session", sess.ID, "err", err)
					} else if changed {
						s.persistMeta(sess)
						announceResumeID()
					}
				}
			}

			// Folder-trust dialog, only for folders the user listed.
			if trust != nil && !trust.done {
				keys, action := trust.next(screen.String(), time.Now())
				if action != "" {
					slog.Info("pty: trust prompt", "session", sess.ID, "action", action)
				}
				if keys != nil {
					if !pw.Send(ctx, writeUnit{parts: [][]byte{keys}}) {
						slog.Warn("pty: trust prompt not sent", "session", sess.ID)
					}
				}
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
	out := append(buf, b...) //nolint:gocritic // appendAssign false positive: the only caller reassigns buf from the result.
	if len(out) > cap {
		out = out[len(out)-cap:]
	}
	return out
}

// errReapTimeout is reported when the child has not exited StopDrainTimeout
// after it was killed.
var errReapTimeout = errors.New("pty: reap timed out")

// collectExit reaps the child once the reader has stopped and classifies the
// exit. rerr is the reader's result: nil means the child hung up the PTY, so
// an unbounded Wait returns promptly. A non-nil rerr (a read error, or a
// transcript write error such as a full disk) means the child is still
// running: it is killed and reaped within StopDrainTimeout, and the session
// fails. Without the kill, Wait would block until the child chose to exit and
// Delete would hang behind it.
func (s *Supervisor) collectExit(id string, cmd *exec.Cmd, f console, rerr error) (final protocol.State, exitCode *int, waitErr error) {
	if rerr != nil {
		slog.Error("pty: reader failed; killing child", "session", id, "pid", cmd.Process.Pid, "err", rerr)
		return protocol.StateFailed, nil, s.killAndReapNow(id, cmd, f)
	}
	waitErr = f.Wait()
	if waitErr == nil {
		return protocol.StateDone, nil, nil
	}
	if ee := new(exec.ExitError); errors.As(waitErr, &ee) {
		code := ee.ExitCode()
		if code != 0 {
			return protocol.StateFailed, &code, waitErr
		}
		return protocol.StateDone, &code, waitErr
	}
	return protocol.StateDone, nil, waitErr
}

// killTree kills the child's whole process tree (console.KillTree) and falls
// back to the leader alone when that fails.
func (s *Supervisor) killTree(id string, cmd *exec.Cmd, f console) {
	if err := f.KillTree(); err != nil {
		slog.Warn("pty: kill process tree failed; killing pid", "session", id, "pid", cmd.Process.Pid, "err", err)
		_ = cmd.Process.Kill()
	}
}

// killAndReap kills the child's whole process tree (console.KillTree) —
// agents spawn MCP servers and tool shells that inherit the PTY and would
// otherwise keep the reader from seeing EOF. The reader drain and the reap are
// both bounded by StopDrainTimeout; on timeout Run returns anyway and the
// deferred PTY close unblocks the reader.
func (s *Supervisor) killAndReap(id string, cmd *exec.Cmd, f console, errc <-chan error) {
	pid := cmd.Process.Pid
	s.killTree(id, cmd, f)
	timeout := time.NewTimer(s.cfg.StopDrainTimeout)
	defer timeout.Stop()
	select {
	case <-errc:
	case <-timeout.C:
		slog.Error("pty: stop drain timed out; PTY still held open", "session", id, "pid", pid, "timeout", s.cfg.StopDrainTimeout)
		return
	}
	_ = s.reap(id, pid, f, timeout.C)
}

// killAndReapNow is killAndReap for when no reader is running (it never
// started, or it already stopped on an error). It returns the child's wait
// error, or errReapTimeout.
//
// Something must still read the PTY: on macOS a killed process blocks in
// its terminal close until unread output drains, so without a reader the
// reap times out on any child that printed. The drain goroutine discards
// output and ends at EOF/hangup, or when Run's deferred f.Close fails its
// Read.
func (s *Supervisor) killAndReapNow(id string, cmd *exec.Cmd, f console) error {
	go func() { _, _ = io.Copy(io.Discard, f) }()
	s.killTree(id, cmd, f)
	timeout := time.NewTimer(s.cfg.StopDrainTimeout)
	defer timeout.Stop()
	return s.reap(id, cmd.Process.Pid, f, timeout.C)
}

// reap waits for the child until timeout fires. On timeout the waiting
// goroutine stays parked in Wait until the child finally exits; its result
// channel is buffered so it never blocks after that.
func (s *Supervisor) reap(id string, pid int, f console, timeout <-chan time.Time) error {
	waited := make(chan error, 1)
	go func() { waited <- f.Wait() }()
	select {
	case err := <-waited:
		return err
	case <-timeout:
		slog.Error("pty: reap timed out", "session", id, "pid", pid)
		return errReapTimeout
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
	if n, ok := s.cfg.Adapter.(adapter.Noticer); ok {
		if notice := n.Notice(); notice != "" {
			last = notice // signed out: say so instead of the agent's last row
		}
	}
	if last == "" {
		return
	}
	if err := s.cfg.Store.UpdateLastLine(id, last); err != nil {
		slog.Warn("pty: update last_line failed", "session", id, "err", err)
	}
}

// envWithout returns env minus the variables named in drop.
func envWithout(env, drop []string) []string {
	if len(drop) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(drop, func(d string) bool { return sameEnvName(d, name) }) {
			out = append(out, kv)
		}
	}
	return out
}

// sameEnvName compares variable names; Windows ignores case.
func sameEnvName(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
