package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/loop"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// LoopService is what the loop intents call. The daemon's implementation
// (internal/daemon/boot) refuses with loop.ErrOff while the loops feature is
// off. Errors should be *loop.Error: the handlers map their Kind to a code.
type LoopService interface {
	Start(protocol.StartLoop) (protocol.LoopSummary, error)
	Pause(id string) (protocol.LoopSummary, error)
	Resume(id string) (protocol.LoopSummary, error)
	Stop(id string) (protocol.LoopSummary, error)
	// Snapshot lists the loops for a joining client; nil while off.
	Snapshot() []protocol.LoopSummary
	// UserInput is told of a client keystroke for a session. It must not
	// block.
	UserInput(sessionID string)
}

func (s *Server) loopSnapshot() []protocol.LoopSummary {
	if s.cfg.Loops == nil {
		return nil
	}
	return s.cfg.Loops.Snapshot()
}

func (s *Server) userInput(sessionID string) {
	if s.cfg.Loops != nil {
		s.cfg.Loops.UserInput(sessionID)
	}
}

// SubscribeLoops registers a global callback for Loop events. Returns a
// cancel func. fn follows the SubscribeSessionOutput contract: it must not
// block or (un)subscribe.
func (s *Server) SubscribeLoops(fn func(protocol.LoopSummary)) func() {
	s.loopMu.Lock()
	defer s.loopMu.Unlock()
	idx := len(s.loopSubs)
	s.loopSubs = append(s.loopSubs, fn)
	return func() {
		s.loopMu.Lock()
		defer s.loopMu.Unlock()
		if idx < len(s.loopSubs) {
			s.loopSubs[idx] = nil
		}
	}
}

// PublishLoop sends a loop's new state to every connected client.
func (s *Server) PublishLoop(l protocol.LoopSummary) {
	s.loopMu.RLock()
	defer s.loopMu.RUnlock()
	for _, fn := range s.loopSubs {
		if fn != nil {
			fn(l)
		}
	}
}

// SpawnLoopSession starts a loop's session through the normal NewSession
// path, owned by the loop.
func (s *Server) SpawnLoopSession(ctx context.Context, req loop.SpawnRequest) (string, error) {
	return spawnSession(ctx, "", protocol.NewSession{
		ToolID: req.Tool, ModelID: req.Model, Effort: req.Effort,
		Slug: req.Slug, Title: req.Title, CWD: req.CWD, InitialPrompt: req.Prompt,
	}, s, req.LoopID, req.Iter)
}

// SendText types text into a session and presses Enter, as a Reply does,
// without counting as the user's activity.
func (s *Server) SendText(sessionID, text string) error {
	ch := s.InputChannel(sessionID)
	if ch == nil {
		return errors.New("session not running")
	}
	return typeText(ch, text)
}

// SetSessionLoop records the loop and turn on a session and persists it.
func (s *Server) SetSessionLoop(sessionID, loopID string, iter int) {
	if err := s.cfg.Store.SetLoop(sessionID, loopID, iter); err != nil {
		return
	}
	if sess, ok := s.cfg.Store.Get(sessionID); ok {
		_ = state.WriteMeta(s.cfg.StateDir, sess)
	}
}

// EnsureLive respawns an exited session in place (as Resume does); a live
// session is left alone.
func (s *Server) EnsureLive(ctx context.Context, sessionID string) error {
	sess, ok := s.cfg.Store.Get(sessionID)
	if !ok {
		return errors.New("session not found")
	}
	if s.hasStop(sessionID) || !state.IsTerminal(sess.Summary().State) {
		return nil
	}
	if !s.resumeOn() {
		return errors.New("resume is off")
	}
	resumed, err := s.resumeExited(ctx, sess)
	if err != nil {
		return err
	}
	if !resumed {
		return nil // lost the race: it is live now
	}
	return nil
}

// loopReplyErr maps a LoopService error to its Error code.
func loopReplyErr(err error) error {
	var le *loop.Error
	if !errors.As(err, &le) {
		return replyErr(protocol.ErrCodeBadIntent, err)
	}
	switch le.Kind {
	case loop.KindBadState:
		return replyErr(protocol.ErrCodeBadState, err)
	case loop.KindSpawn:
		switch {
		case isBadIntent(le.Err):
			return replyErr(protocol.ErrCodeBadIntent, err)
		case strings.Contains(err.Error(), "too many concurrent sessions"):
			return replyErr(protocol.ErrCodeTooManySessions, err)
		}
		return replyErr(protocol.ErrCodeSpawn, err)
	case loop.KindBadInput, loop.KindNotFound:
		return replyErr(protocol.ErrCodeBadIntent, err)
	}
	return replyErr(protocol.ErrCodeBadIntent, err)
}

func (c *client) loopService() (LoopService, error) {
	if c.srv.cfg.Loops == nil {
		return nil, replyMsg(protocol.ErrCodeBadIntent, "intent not implemented")
	}
	return c.srv.cfg.Loops, nil
}

func handleStartLoop(c *client, env protocol.Envelope) (outcome, error) {
	svc, err := c.loopService()
	if err != nil {
		return outcome{}, err
	}
	p, err := decodeIntent[protocol.StartLoop](env)
	if err != nil {
		return outcome{}, err
	}
	sum, err := svc.Start(p)
	if err != nil {
		return outcome{}, loopReplyErr(err)
	}
	c.sendEvent(protocol.EventLoop, env.ID, sum)
	return outcome{replied: true}, nil
}

func handlePauseLoop(c *client, env protocol.Envelope) (outcome, error) {
	return loopControl(c, env, func(svc LoopService, id string) (protocol.LoopSummary, error) { return svc.Pause(id) },
		func(p protocol.PauseLoop) string { return p.LoopID })
}

func handleResumeLoop(c *client, env protocol.Envelope) (outcome, error) {
	return loopControl(c, env, func(svc LoopService, id string) (protocol.LoopSummary, error) { return svc.Resume(id) },
		func(p protocol.ResumeLoop) string { return p.LoopID })
}

func handleStopLoop(c *client, env protocol.Envelope) (outcome, error) {
	return loopControl(c, env, func(svc LoopService, id string) (protocol.LoopSummary, error) { return svc.Stop(id) },
		func(p protocol.StopLoop) string { return p.LoopID })
}

func loopControl[P any](c *client, env protocol.Envelope, do func(LoopService, string) (protocol.LoopSummary, error), id func(P) string) (outcome, error) {
	svc, err := c.loopService()
	if err != nil {
		return outcome{}, err
	}
	p, err := decodeIntent[P](env)
	if err != nil {
		return outcome{}, err
	}
	if !protocol.ValidLoopID(id(p)) {
		return outcome{}, replyErr(protocol.ErrCodeBadIntent, fmt.Errorf("invalid loop id"))
	}
	sum, err := do(svc, id(p))
	if err != nil {
		return outcome{}, loopReplyErr(err)
	}
	c.sendEvent(protocol.EventLoop, env.ID, sum)
	return outcome{replied: true}, nil
}
