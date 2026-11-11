package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestUsageQuery_ReturnsTheReport(t *testing.T) {
	c, fd := pipeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type res struct {
		rep protocol.UsageReport
		err error
	}
	ch := make(chan res, 1)
	go func() {
		rep, err := c.UsageQuery(ctx, protocol.UsageQuery{Range: "30d", Tool: "claude"})
		ch <- res{rep, err}
	}()
	env := fd.read(t)
	require.Equal(t, protocol.IntentUsageQuery, env.Type)
	fd.event(t, protocol.EventUsageResult, env.ID, protocol.UsageReport{Range: "30d", Total: protocol.UsageRow{Sessions: 3}})
	got := <-ch
	require.NoError(t, got.err)
	require.Equal(t, "30d", got.rep.Range)
	require.Equal(t, 3, got.rep.Total.Sessions)
}

// The old-daemon path: bad_intent "intent not implemented" reads as an
// instruction to restart the daemon.
func TestUsageQuery_OldDaemon(t *testing.T) {
	c, fd := pipeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	go func() { _, err := c.UsageQuery(ctx, protocol.UsageQuery{}); ch <- err }()
	env := fd.read(t)
	fd.event(t, protocol.EventError, env.ID, protocol.ErrorEvent{ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: "intent not implemented"})
	err := <-ch
	var ce *Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, protocol.ErrCodeBadIntent, ce.Code)
	require.Contains(t, err.Error(), "rex daemon restart")
}

func TestUsageQuery_WrongReplyType(t *testing.T) {
	c, fd := pipeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	go func() { _, err := c.UsageQuery(ctx, protocol.UsageQuery{}); ch <- err }()
	env := fd.read(t)
	fd.ack(t, env, "")
	require.ErrorContains(t, <-ch, "unexpected reply")
}
