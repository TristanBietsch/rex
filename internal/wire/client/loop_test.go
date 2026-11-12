package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestLoopCalls_ReturnTheLoop(t *testing.T) {
	cases := []struct {
		intent string
		call   func(*Client, context.Context) (protocol.LoopSummary, error)
	}{
		{protocol.IntentStartLoop, func(c *Client, ctx context.Context) (protocol.LoopSummary, error) {
			return c.StartLoop(ctx, protocol.StartLoop{Name: "fix"})
		}},
		{protocol.IntentPauseLoop, func(c *Client, ctx context.Context) (protocol.LoopSummary, error) {
			return c.PauseLoop(ctx, "loop-00000001")
		}},
		{protocol.IntentResumeLoop, func(c *Client, ctx context.Context) (protocol.LoopSummary, error) {
			return c.ResumeLoop(ctx, "loop-00000001")
		}},
		{protocol.IntentStopLoop, func(c *Client, ctx context.Context) (protocol.LoopSummary, error) {
			return c.StopLoop(ctx, "loop-00000001")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.intent, func(t *testing.T) {
			c, fd := pipeClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			type res struct {
				l   protocol.LoopSummary
				err error
			}
			ch := make(chan res, 1)
			go func() { l, err := tc.call(c, ctx); ch <- res{l, err} }()
			env := fd.read(t)
			require.Equal(t, tc.intent, env.Type)
			fd.event(t, protocol.EventLoop, env.ID, protocol.LoopSummary{ID: "loop-00000001", State: protocol.LoopRunning})
			got := <-ch
			require.NoError(t, got.err)
			require.Equal(t, protocol.LoopRunning, got.l.State)
		})
	}
}

// The old-daemon path: bad_intent "intent not implemented" reads as an
// instruction to restart the daemon.
func TestLoopCalls_OldDaemon(t *testing.T) {
	c, fd := pipeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	go func() { _, err := c.StartLoop(ctx, protocol.StartLoop{Name: "x"}); ch <- err }()
	env := fd.read(t)
	fd.event(t, protocol.EventError, env.ID, protocol.ErrorEvent{ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: "intent not implemented"})
	err := <-ch
	var ce *Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, protocol.ErrCodeBadIntent, ce.Code)
	require.Contains(t, err.Error(), "rex daemon restart")
}

func TestLoopCalls_WrongReplyAndBadPayload(t *testing.T) {
	c, fd := pipeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	go func() { _, err := c.PauseLoop(ctx, "loop-00000001"); ch <- err }()
	env := fd.read(t)
	fd.ack(t, env, "")
	require.ErrorContains(t, <-ch, "unexpected reply")

	go func() { _, err := c.StopLoop(ctx, "loop-00000001"); ch <- err }()
	env = fd.read(t)
	fd.event(t, protocol.EventLoop, env.ID, "not a loop")
	require.ErrorContains(t, <-ch, "decode reply")
}
