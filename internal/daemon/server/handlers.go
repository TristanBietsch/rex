package server

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
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
		protocol.IntentSetSessionFleet:  handleSetSessionFleet,
		protocol.IntentFocusFilter:      handleFocusFilter,
		protocol.IntentSetMaxConcurrent: handleSetMaxConcurrent,
		protocol.IntentShutdown:         handleShutdown,
		protocol.IntentReload:           handleReload,
	}
}

func handleHello(c *client, env protocol.Envelope) {
	snap := protocol.Snapshot{Sessions: c.srv.cfg.Store.Snapshot(), Filter: "all"}
	c.sendEvent(protocol.EventSnapshot, env.ID, snap)
	c.subscribed.Store(true)
}

func handleNewSessionIntent(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.NewSession](c, env)
	if !ok {
		return
	}
	if err := handleNewSession(c.ctx, env.ID, p, c.srv); err != nil {
		code := protocol.ErrCodeSpawn
		switch {
		case isBadIntent(err):
			code = protocol.ErrCodeBadIntent
		case strings.Contains(err.Error(), "too many concurrent sessions"):
			code = protocol.ErrCodeTooManySessions
		}
		c.sendError(env.ID, code, err.Error())
	}
}

func handleDelete(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.Delete](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	// Stops a running supervisor first so its goroutines don't race with
	// the on-disk cleanup; disk cleanup is best-effort.
	if err := c.srv.deleteSession(p.SessionID, protocol.RemovedByUser); err != nil {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
	}
}

func handleComplete(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.Complete](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	if _, ok := c.srv.cfg.Store.Get(p.SessionID); !ok {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
		return
	}
	c.srv.CompleteSession(p.SessionID)
}

func handleSubscribe(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.Subscribe](c, env)
	if !ok || p.SessionID == "" || !c.validSessionID(env, p.SessionID) {
		return
	}
	srv := c.srv
	// Only sessions the store knows get a replay or a sink: an unknown id
	// would otherwise read whatever transcript sits at that path and leave
	// a sink nobody ever drops.
	if _, ok := srv.cfg.Store.Get(p.SessionID); !ok {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
		return
	}
	var preambleBytes int
	if p.Replay {
		tail, err := state.TranscriptTail(srv.TranscriptDir(), p.SessionID, transcriptReplayMax)
		if err != nil {
			slog.Warn("server: transcript replay failed", "session", p.SessionID, "err", err)
		}
		if len(tail) > 0 {
			c.sendEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
				SessionID: p.SessionID, Bytes: tail,
			})
		}
	} else if p.ModeSync {
		// The replay already carries the child's mode switches;
		// without one, send them as a preamble.
		if pre := srv.ModePreamble(p.SessionID); len(pre) > 0 {
			preambleBytes = len(pre)
			c.sendEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
				SessionID: p.SessionID, Bytes: pre,
			})
		}
	}
	slog.Info("server: subscribe", "session", p.SessionID, "replay", p.Replay,
		"mode_sync", p.ModeSync, "preamble_bytes", preambleBytes)
	// Register an output sink for this session for the rest of the connection.
	c.cleanups = append(c.cleanups, srv.SubscribeSessionOutput(p.SessionID, func(b []byte) {
		c.sendEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
			SessionID: p.SessionID, Bytes: b,
		})
	}))
}

func handleResize(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.Resize](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	if p.Cols == 0 || p.Rows == 0 {
		return
	}
	if err := c.srv.Resize(p.SessionID, p.Cols, p.Rows); err != nil {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
	}
}

func handleSendInput(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.SendInput](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	ch := c.srv.InputChannel(p.SessionID)
	if ch == nil {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not running")
		return
	}
	select {
	case ch <- p.Bytes:
		if !p.Synthetic {
			c.srv.cancelRetireOnInput(p.SessionID, "SendInput")
		}
	default:
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "input buffer full")
	}
}

func handleReply(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.Reply](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	ch := c.srv.InputChannel(p.SessionID)
	if ch == nil {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not running")
		return
	}
	// Text, then Enter as a separate write. Terminals send Enter as
	// \r (the PTY maps it to \n for line REPLs); full-screen TUIs like
	// Claude Code insert a newline on \n instead of submitting, and
	// treat text+\r in one burst as a paste.
	select {
	case ch <- []byte(p.Text):
	default:
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "input buffer full")
		return
	}
	time.Sleep(replySubmitDelay)
	select {
	case ch <- []byte{'\r'}:
		slog.Debug("server: reply submitted", "session", p.SessionID, "bytes", len(p.Text))
		c.srv.cancelRetireOnInput(p.SessionID, "Reply")
	default:
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "input buffer full")
	}
}

func handleRename(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.Rename](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	if err := validateRename(p); err != nil {
		slog.Warn("server: rename rejected", "session", p.SessionID, "slug", p.Slug, "reason", err, "req_id", env.ID)
		c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
		return
	}
	store := c.srv.cfg.Store
	sess, ok := store.Get(p.SessionID)
	if !ok {
		slog.Warn("server: rename rejected", "session", p.SessionID, "reason", "unknown session", "req_id", env.ID)
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
		return
	}
	if err := store.UpdateRename(p.SessionID, p.Slug, p.Title); err != nil {
		var taken *state.SlugTakenError
		if errors.As(err, &taken) {
			slog.Warn("server: rename rejected, slug taken", "session", p.SessionID,
				"slug", p.Slug, "holder", taken.ShortID, "req_id", env.ID)
			c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
			return
		}
		slog.Warn("server: rename failed", "session", p.SessionID, "err", err, "req_id", env.ID)
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
		return
	}
	if err := state.WriteMeta(c.srv.cfg.StateDir, sess); err != nil {
		slog.Warn("server: write meta after rename", "session", p.SessionID, "err", err)
	}
	slog.Info("server: rename", "session", p.SessionID, "slug", p.Slug, "title_len", len(p.Title), "req_id", env.ID)
}

func handleSetSessionName(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.SetSessionName](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	name, err := protocol.NormalizeSessionName(p.Name)
	if err != nil {
		slog.Warn("server: set_name rejected", "session", p.SessionID, "reason", err, "req_id", env.ID)
		c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
		return
	}
	sess, ok := c.srv.cfg.Store.Get(p.SessionID)
	if !ok {
		slog.Warn("server: set_name rejected", "session", p.SessionID, "reason", "unknown session", "req_id", env.ID)
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
		return
	}
	changed, err := c.srv.cfg.Store.SetName(p.SessionID, name)
	if err != nil {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
		return
	}
	// Broadcast even when unchanged: it is the confirmation clients wait on.
	c.srv.commit(sess, map[string]any{"name": name})
	slog.Info("server: set_name", "session", p.SessionID, "cleared", name == "",
		"runes", len([]rune(name)), "changed", changed, "req_id", env.ID)
}

func handleRetire(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.Retire](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	srv := c.srv
	at, ttl, err := srv.retireDeadline(p.TTLSeconds)
	if err != nil {
		slog.Warn("retire: invalid ttl", "session", p.SessionID, "ttl_s", p.TTLSeconds, "err", err)
		c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
		return
	}
	sess, ok := srv.cfg.Store.Get(p.SessionID)
	if !ok {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
		return
	}
	markedAt := srv.now().Round(0).UTC()
	changed, err := srv.cfg.Store.SetRetire(p.SessionID, at, markedAt)
	if err != nil {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
		return
	}
	if !changed {
		slog.Debug("retire: already marked; no-op", "session", p.SessionID)
		return
	}
	srv.commit(sess, map[string]any{"retire_at": at, "retired_at": markedAt})
	sum := sess.Summary()
	slog.Info("retire: marked", "session", p.SessionID, "slug", sum.Slug, "state", sum.State,
		"ttl_s", int64(ttl.Seconds()), "retire_at", at, "retired_at", markedAt)
}

func handleUnretire(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.Unretire](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	store := c.srv.cfg.Store
	sess, ok := store.Get(p.SessionID)
	if !ok {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
		return
	}
	remaining := time.Until(sess.Summary().RetireAt)
	changed, err := store.ClearRetire(p.SessionID)
	if errors.Is(err, state.ErrReaping) {
		slog.Warn("retire: unmark rejected; clearing", "session", p.SessionID)
		c.sendError(env.ID, protocol.ErrCodeBadState, err.Error())
		return
	}
	if err != nil {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
		return
	}
	if changed {
		c.srv.commit(sess, map[string]any{"retire_at": nil, "retired_at": nil})
		slog.Info("retire: unmarked", "session", p.SessionID, "cause", "user", "remaining_s", int64(remaining.Seconds()))
	}
}

func handleSetSessionFleet(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.SetSessionFleet](c, env)
	if !ok || !c.validSessionID(env, p.SessionID) {
		return
	}
	store := c.srv.cfg.Store
	sess, ok := store.Get(p.SessionID)
	if !ok {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
		return
	}
	if err := store.SetFleet(p.SessionID, p.Fleet); err != nil {
		c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
		return
	}
	if err := state.WriteMeta(c.srv.cfg.StateDir, sess); err != nil {
		slog.Warn("server: write meta after fleet update", "session", p.SessionID, "err", err)
	}
	slog.Info("server: fleet updated", "session", p.SessionID, "fleet", p.Fleet)
}

// handleFocusFilter accepts the cosmetic FocusFilter intent silently.
func handleFocusFilter(*client, protocol.Envelope) {}

func handleSetMaxConcurrent(c *client, env protocol.Envelope) {
	p, ok := decodeIntent[protocol.SetMaxConcurrent](c, env)
	if !ok {
		return
	}
	c.srv.SetMaxConcurrentSessions(p.N)
}

// Shutdown and Reload are the socket forms of SIGTERM and SIGHUP, for
// platforms (Windows) that cannot signal the daemon.

func handleShutdown(c *client, env protocol.Envelope) {
	if c.srv.cfg.OnShutdown == nil {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
		return
	}
	slog.Info("server: shutdown requested by client")
	c.srv.cfg.OnShutdown()
}

func handleReload(c *client, env protocol.Envelope) {
	if c.srv.cfg.OnReload == nil {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
		return
	}
	if err := c.srv.cfg.OnReload(); err != nil {
		c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
	}
}
