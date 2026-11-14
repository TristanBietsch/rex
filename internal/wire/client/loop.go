package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// StartLoop starts the loop defined by the named loop file and returns the
// new loop. A daemon that predates the loop intents answers bad_intent
// "intent not implemented", which the returned *Error renders as "run 'rex
// daemon restart'".
func (c *Client) StartLoop(ctx context.Context, p protocol.StartLoop) (protocol.LoopSummary, error) {
	return c.loopCall(ctx, protocol.IntentStartLoop, p)
}

// PauseLoop pauses a running loop.
func (c *Client) PauseLoop(ctx context.Context, loopID string) (protocol.LoopSummary, error) {
	return c.loopCall(ctx, protocol.IntentPauseLoop, protocol.PauseLoop{LoopID: loopID})
}

// ResumeLoop continues a paused loop.
func (c *Client) ResumeLoop(ctx context.Context, loopID string) (protocol.LoopSummary, error) {
	return c.loopCall(ctx, protocol.IntentResumeLoop, protocol.ResumeLoop{LoopID: loopID})
}

// StopLoop ends a loop for good.
func (c *Client) StopLoop(ctx context.Context, loopID string) (protocol.LoopSummary, error) {
	return c.loopCall(ctx, protocol.IntentStopLoop, protocol.StopLoop{LoopID: loopID})
}

func (c *Client) loopCall(ctx context.Context, intent string, payload any) (protocol.LoopSummary, error) {
	env, err := c.Do(ctx, intent, payload)
	if err != nil {
		return protocol.LoopSummary{}, err
	}
	if env.Type != protocol.EventLoop {
		return protocol.LoopSummary{}, fmt.Errorf("%s: unexpected reply %q", intent, env.Type)
	}
	var l protocol.LoopSummary
	if err := json.Unmarshal(env.Data, &l); err != nil {
		return protocol.LoopSummary{}, fmt.Errorf("%s: decode reply: %w", intent, err)
	}
	return l, nil
}
