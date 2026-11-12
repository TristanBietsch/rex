package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// replySubmitDelay separates a Reply's text from its Enter keystroke so TUIs
// commit the text to their input box before submitting (matches the initial
// prompt paste path in pty.Supervisor).
const replySubmitDelay = 120 * time.Millisecond

// defaultHandlers is the intent table every Server starts with.
func defaultHandlers() map[string]intentHandler {
	return map[string]intentHandler{
		protocol.IntentHello:            handleHello,
		protocol.IntentNewSession:       handleNewSessionIntent,
		protocol.IntentDelete:           handleDelete,
		protocol.IntentComplete:         handleComplete,
		protocol.IntentSubscribe:        handleSubscribe,
		protocol.IntentResize:           handleResize,
		protocol.IntentSendInput:        handleSendInput,
		protocol.IntentReply:            handleReply,
		protocol.IntentRename:           handleRename,
		protocol.IntentSetSessionName:   handleSetSessionName,
		protocol.IntentRetire:           handleRetire,
		protocol.IntentUnretire:         handleUnretire,
		protocol.IntentResume:           handleResume,
		protocol.IntentSetSessionFleet:  handleSetSessionFleet,
		protocol.IntentFocusFilter:      handleFocusFilter,
		protocol.IntentSetMaxConcurrent: handleSetMaxConcurrent,
		protocol.IntentShutdown:         handleShutdown,
		protocol.IntentReload:           handleReload,
		protocol.IntentRestart:          handleRestart,
		protocol.IntentSwapBoards:       handleSwapBoards,
		protocol.IntentBoards:           handleBoards,
		protocol.IntentUndo:             handleUndo,
		protocol.IntentRedo:             handleRedo,
		protocol.IntentUsageSample:      handleUsageSample,
		protocol.IntentUsageQuery:       handleUsageQuery,
		protocol.IntentStartLoop:        handleStartLoop,
		protocol.IntentPauseLoop:        handlePauseLoop,
		protocol.IntentResumeLoop:       handleResumeLoop,
		protocol.IntentStopLoop:         handleStopLoop,
	}
}

func handleHello(c *client, env protocol.Envelope) (outcome, error) {
	// The payload only feeds the log; a client that sends none still gets
	// its snapshot, as before.
	var p protocol.Hello
	_ = json.Unmarshal(env.Data, &p)
	slog.Debug("server: hello", "conn", c.out.id, "client", p.Client, "client_version", p.ClientVersion)
	snap := protocol.Snapshot{
		Sessions:      c.srv.cfg.Store.Snapshot(),
		Filter:        "all",
		DaemonVersion: version.Version,
		// An overview published between this read and subscribed.Store below
		// is missed until the next one (documented in protocol.md).
		Overview: c.srv.Overview(),
		// Same race as the overview: a change between this read and the
		// subscription below is missed until the next one.
		Usage: c.srv.Usage(),
		// Loops are read here too; a change after this is a Loop event.
		Loops: c.srv.loopSnapshot(),
		Tools: c.srv.ToolInfos(),
	}
	snap.WorkspaceTagging = c.srv.cfg.Workspaces != nil && c.srv.cfg.Workspaces.Enabled()
	snap.DaemonPID = os.Getpid()
	snap.DaemonBuild = c.srv.cfg.Build
	snap.DaemonStartedAt = c.srv.cfg.StartedAt
	if p.Client == protocol.ClientTUI {
		c.joinBoard()
		c.out.setBoardInfo(protocol.BoardInfo{PID: p.PID, Build: p.Build})
	}
	c.sendEvent(protocol.EventSnapshot, env.ID, snap)
	c.subscribed.Store(true)
	if p.Client == protocol.ClientTUI && c.srv.cfg.OnBoardClient != nil {
		c.srv.cfg.OnBoardClient()
	}
	return outcome{replied: true}, nil
}

func handleNewSessionIntent(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeIntent[protocol.NewSession](env)
	if err != nil {
		return outcome{}, err
	}
	id, err := handleNewSession(c.ctx, env.ID, p, c.srv)
	if err != nil {
		code := protocol.ErrCodeSpawn
		switch {
		case isBadIntent(err):
			code = protocol.ErrCodeBadIntent
		case strings.Contains(err.Error(), "too many concurrent sessions"):
			code = protocol.ErrCodeTooManySessions
		}
		return outcome{}, replyErr(code, err)
	}
	return outcome{sessionID: id}, nil
}

func handleDelete(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Delete) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	// Stops a running supervisor first so its goroutines don't race with
	// the on-disk cleanup; disk cleanup is best-effort.
	if err := c.srv.deleteSession(p.SessionID, protocol.RemovedByUser); err != nil {
		if errors.Is(err, errAuditFailed) {
			return outcome{}, replyErr(protocol.ErrCodeInternal, err)
		}
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	return outcome{sessionID: p.SessionID}, nil
}

func handleComplete(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Complete) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	if _, ok := c.srv.cfg.Store.Get(p.SessionID); !ok {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	c.srv.CompleteSession(p.SessionID)
	return outcome{sessionID: p.SessionID}, nil
}

func handleSubscribe(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeIntent[protocol.Subscribe](env)
	if err != nil || p.SessionID == "" {
		return outcome{}, err // no session: board-wide events only, nothing to do
	}
	if err := checkSessionID(p.SessionID); err != nil {
		return outcome{}, err
	}
	srv := c.srv
	// Only sessions the store knows get a replay or a sink: an unknown id
	// would otherwise read whatever transcript sits at that path and leave
	// a sink nobody ever drops.
	if _, ok := srv.cfg.Store.Get(p.SessionID); !ok {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	var replayBytes, repaintBytes, preambleBytes int
	// With the session's output held, the replay, the repaint and the sink
	// registration see every PTY chunk exactly once: in the transcript tail
	// and on the screen, or live through the sink.
	srv.withOutputHeld(p.SessionID, func(repaint func() []byte) {
		if p.Replay {
			tail, err := state.TranscriptTail(srv.TranscriptDir(), p.SessionID, transcriptReplayMax)
			if err != nil {
				slog.Warn("server: transcript replay failed", "session", p.SessionID, "err", err)
			}
			if len(tail) > 0 {
				replayBytes = len(tail)
				c.sendEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
					SessionID: p.SessionID, Bytes: tail,
				})
			}
		}
		switch {
		case p.Repaint:
			// The current screen, modes included: the client's emulator
			// shows what the agent last drew even if the agent does not
			// draw again (busy, or deaf to the ^L nudge).
			if b := repaint(); len(b) > 0 {
				repaintBytes = len(b)
				c.sendEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
					SessionID: p.SessionID, Bytes: b, Repaint: true,
				})
			}
		case p.ModeSync && !p.Replay:
			// The replay already carries the child's mode switches;
			// without one, send them as a preamble.
			if pre := srv.ModePreamble(p.SessionID); len(pre) > 0 {
				preambleBytes = len(pre)
				c.sendEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
					SessionID: p.SessionID, Bytes: pre,
				})
			}
		}
		// Register an output sink for this session for the rest of the connection.
		c.cleanups = append(c.cleanups, srv.SubscribeSessionOutput(p.SessionID, func(b []byte) {
			c.sendEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
				SessionID: p.SessionID, Bytes: b,
			})
		}))
	})
	slog.Info("server: subscribe", "session", p.SessionID, "replay", p.Replay, "replay_bytes", replayBytes,
		"repaint", p.Repaint, "repaint_bytes", repaintBytes, "mode_sync", p.ModeSync, "preamble_bytes", preambleBytes)
	return outcome{sessionID: p.SessionID}, nil
}

func handleResize(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Resize) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	if p.Cols == 0 || p.Rows == 0 {
		return outcome{sessionID: p.SessionID}, nil // ignored, as documented
	}
	// A client-chosen size sizes the daemon's screen mirror: clamp it.
	cols, rows := protocol.ClampTermSize(p.Cols, p.Rows)
	if err := c.srv.Resize(p.SessionID, cols, rows); err != nil {
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	return outcome{sessionID: p.SessionID}, nil
}

func handleSendInput(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.SendInput) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	ch := c.srv.InputChannel(p.SessionID)
	if ch == nil {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not running")
	}
	// Under the session's input lock, so the bytes never land between a
	// loop's text and its Enter. The loop is told before they are queued.
	defer c.srv.lockInput(p.SessionID)()
	if !p.Synthetic {
		c.srv.userInput(p.SessionID) // a loop holds its re-sends
	}
	// Queued on the session's single writer, as one unit.
	if _, err := c.srv.writeInput(p.SessionID, pty.WriteUnit{Parts: [][]byte{p.Bytes}}, inputQueueTimeout); err != nil {
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	if !p.Synthetic {
		c.srv.cancelRetireOnInput(p.SessionID, "SendInput")
	}
	return outcome{sessionID: p.SessionID}, nil
}

func handleReply(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Reply) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	ch := c.srv.InputChannel(p.SessionID)
	if ch == nil {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not running")
	}
	defer c.srv.lockInput(p.SessionID)()
	c.srv.userInput(p.SessionID) // a loop holds its re-sends
	// The text and its Enter are one unit on the session's writer: no loop
	// text, client byte or terminal reply lands between them.
	unit := pty.WriteUnit{Parts: [][]byte{[]byte(p.Text), {'\r'}}, Gap: replySubmitDelay, Wait: true}
	if _, err := c.srv.writeInput(p.SessionID, unit, inputWaitTimeout); err != nil {
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	slog.Debug("server: reply submitted", "session", p.SessionID, "bytes", len(p.Text))
	c.srv.cancelRetireOnInput(p.SessionID, "Reply")
	return outcome{sessionID: p.SessionID}, nil
}

func handleRename(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Rename) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	p, err = normalizeRename(p)
	if err == nil {
		err = validateRename(p)
	}
	if err != nil {
		slog.Warn("server: rename rejected", "session", p.SessionID, "slug", p.Slug, "reason", err, "req_id", env.ID)
		return outcome{}, replyErr(protocol.ErrCodeBadIntent, err)
	}
	store := c.srv.cfg.Store
	sess, ok := store.Get(p.SessionID)
	if !ok {
		slog.Warn("server: rename rejected", "session", p.SessionID, "reason", "unknown session", "req_id", env.ID)
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	if err := store.UpdateRename(p.SessionID, p.Slug, p.Title); err != nil {
		var taken *state.SlugTakenError
		if errors.As(err, &taken) {
			slog.Warn("server: rename rejected, slug taken", "session", p.SessionID,
				"slug", p.Slug, "holder", taken.ShortID, "req_id", env.ID)
			return outcome{}, replyErr(protocol.ErrCodeBadIntent, err)
		}
		slog.Warn("server: rename failed", "session", p.SessionID, "err", err, "req_id", env.ID)
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	if err := state.WriteMeta(c.srv.cfg.StateDir, sess); err != nil {
		slog.Warn("server: write meta after rename", "session", p.SessionID, "err", err)
	}
	slog.Info("server: rename", "session", p.SessionID, "slug", p.Slug, "title_len", len(p.Title), "req_id", env.ID)
	return outcome{sessionID: p.SessionID}, nil
}

func handleSetSessionName(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.SetSessionName) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	name, err := protocol.NormalizeSessionName(p.Name)
	if err != nil {
		slog.Warn("server: set_name rejected", "session", p.SessionID, "reason", err, "req_id", env.ID)
		return outcome{}, replyErr(protocol.ErrCodeBadIntent, err)
	}
	sess, ok := c.srv.cfg.Store.Get(p.SessionID)
	if !ok {
		slog.Warn("server: set_name rejected", "session", p.SessionID, "reason", "unknown session", "req_id", env.ID)
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	changed, err := c.srv.cfg.Store.SetName(p.SessionID, name)
	if err != nil {
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	// Broadcast even when unchanged: clients older than acks wait on it as
	// the confirmation.
	c.srv.commit(sess, map[string]any{"name": name})
	slog.Info("server: set_name", "session", p.SessionID, "cleared", name == "",
		"runes", len([]rune(name)), "changed", changed, "req_id", env.ID)
	return outcome{sessionID: p.SessionID}, nil
}

func handleRetire(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Retire) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	srv := c.srv
	at, ttl, err := srv.retireDeadline(p.TTLSeconds)
	if err != nil {
		slog.Warn("retire: invalid ttl", "session", p.SessionID, "ttl_s", p.TTLSeconds, "err", err)
		return outcome{}, replyErr(protocol.ErrCodeBadIntent, err)
	}
	sess, ok := srv.cfg.Store.Get(p.SessionID)
	if !ok {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	markedAt := srv.now().Round(0).UTC()
	changed, err := srv.cfg.Store.SetRetire(p.SessionID, at, markedAt)
	if err != nil {
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	if !changed {
		slog.Debug("retire: already marked; no-op", "session", p.SessionID)
		return outcome{sessionID: p.SessionID}, nil
	}
	srv.commit(sess, map[string]any{"retire_at": at, "retired_at": markedAt})
	sum := sess.Summary()
	slog.Info("retire: marked", "session", p.SessionID, "slug", sum.Slug, "state", sum.State,
		"ttl_s", int64(ttl.Seconds()), "retire_at", at, "retired_at", markedAt)
	return outcome{sessionID: p.SessionID}, nil
}

func handleUnretire(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Unretire) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	store := c.srv.cfg.Store
	sess, ok := store.Get(p.SessionID)
	if !ok {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	remaining := time.Until(sess.Summary().RetireAt)
	changed, err := store.ClearRetire(p.SessionID)
	if errors.Is(err, state.ErrReaping) {
		slog.Warn("retire: unmark rejected; clearing", "session", p.SessionID)
		return outcome{}, replyErr(protocol.ErrCodeBadState, err)
	}
	if err != nil {
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	if changed {
		c.srv.commit(sess, map[string]any{"retire_at": nil, "retired_at": nil})
		slog.Info("retire: unmarked", "session", p.SessionID, "cause", "user", "remaining_s", int64(remaining.Seconds()))
	}
	return outcome{sessionID: p.SessionID}, nil
}

func handleSetSessionFleet(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.SetSessionFleet) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	fleet, err := sanitizeField("fleet", p.Fleet, protocol.MaxFleetRunes)
	if err != nil {
		return outcome{}, replyErr(protocol.ErrCodeBadIntent, err)
	}
	p.Fleet = fleet
	store := c.srv.cfg.Store
	sess, ok := store.Get(p.SessionID)
	if !ok {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	if err := store.SetFleet(p.SessionID, p.Fleet); err != nil {
		return outcome{}, replyErr(protocol.ErrCodeUnknownSession, err)
	}
	if err := state.WriteMeta(c.srv.cfg.StateDir, sess); err != nil {
		slog.Warn("server: write meta after fleet update", "session", p.SessionID, "err", err)
	}
	slog.Info("server: fleet updated", "session", p.SessionID, "fleet", p.Fleet)
	return outcome{sessionID: p.SessionID}, nil
}

// handleFocusFilter accepts the cosmetic FocusFilter intent; nothing is
// stored.
func handleFocusFilter(*client, protocol.Envelope) (outcome, error) { return outcome{}, nil }

func handleSetMaxConcurrent(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeIntent[protocol.SetMaxConcurrent](env)
	if err != nil {
		return outcome{}, err
	}
	c.srv.SetMaxConcurrentSessions(p.N)
	return outcome{}, nil
}

// Shutdown and Reload are the socket forms of SIGTERM and SIGHUP, for
// platforms (Windows) that cannot signal the daemon.

// handleShutdown acks before it starts the shutdown, which may close this
// connection before the Ack is written (best effort, as documented).
func handleShutdown(c *client, env protocol.Envelope) (outcome, error) {
	if c.srv.cfg.OnShutdown == nil {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, "intent not implemented")
	}
	slog.Info("server: shutdown requested by client")
	c.ack(env, "")
	c.srv.cfg.OnShutdown()
	return outcome{replied: true}, nil
}

// handleRestart runs a hot restart. On success the process image is
// replaced and nothing replies (the connection closes); a refusal or a
// failure is the reply, and the daemon goes on as before.
func handleRestart(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeIntent[protocol.Restart](env)
	if err != nil {
		return outcome{}, err
	}
	if p.Mode != protocol.RestartHot {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, "restart mode must be \"hot\" (a cold restart is Shutdown, then a start)")
	}
	slog.Info("server: hot restart requested by client")
	if err := c.srv.HotRestart(c.ctx, c.out); err != nil {
		slog.Warn("server: hot restart refused", "err", err)
		return outcome{}, replyErr(protocol.ErrCodeRestartRefused, err)
	}
	return outcome{}, nil
}

// handleBoards lists the board connections (pid, build).
func handleBoards(c *client, env protocol.Envelope) (outcome, error) {
	c.sendEvent(protocol.EventBoards, env.ID, protocol.Boards{Boards: c.srv.BoardInfos()})
	return outcome{replied: true}, nil
}

// handleSwapBoards asks every board not on the given build to swap.
func handleSwapBoards(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeIntent[protocol.SwapBoards](env)
	if err != nil {
		return outcome{}, err
	}
	sent := c.srv.SwapBoards(p.Build)
	slog.Info("server: swap requested for boards", "boards", len(sent))
	c.sendEvent(protocol.EventBoards, env.ID, protocol.Boards{Boards: sent})
	return outcome{replied: true}, nil
}

func handleReload(c *client, env protocol.Envelope) (outcome, error) {
	if c.srv.cfg.OnReload == nil {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, "intent not implemented")
	}
	if err := c.srv.cfg.OnReload(); err != nil {
		return outcome{}, replyErr(protocol.ErrCodeBadIntent, err)
	}
	return outcome{}, nil
}

// handleUsageSample takes the usage fields of one Claude status-line render
// from a tap. It is fire and forget (the tap sends no ID); a sample for an
// unknown session, or one that carries nothing, is dropped quietly.
func handleUsageSample(c *client, env protocol.Envelope) (outcome, error) {
	if c.srv.cfg.OnUsageSample == nil {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, "intent not implemented")
	}
	p, err := decodeIntent[protocol.UsageSample](env)
	if err != nil {
		return outcome{}, err
	}
	if p.SessionID == "" || p.Empty() {
		return outcome{}, nil
	}
	if _, ok := c.srv.cfg.Store.Get(p.SessionID); !ok {
		return outcome{}, nil
	}
	c.srv.cfg.OnUsageSample(p)
	return outcome{sessionID: p.SessionID}, nil
}

// handleUsageQuery answers with a UsageResult event carrying the aggregates.
func handleUsageQuery(c *client, env protocol.Envelope) (outcome, error) {
	if c.srv.cfg.UsageQuery == nil {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, "intent not implemented")
	}
	p, err := decodeIntent[protocol.UsageQuery](env)
	if err != nil {
		return outcome{}, err
	}
	if p.Range != "" && !protocol.ValidUsageRange(p.Range) {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, "range must be today, 7d or 30d")
	}
	rep, err := c.srv.cfg.UsageQuery(p)
	if err != nil {
		return outcome{}, replyErr(protocol.ErrCodeBadIntent, err)
	}
	c.sendEvent(protocol.EventUsageResult, env.ID, rep)
	return outcome{replied: true}, nil
}
