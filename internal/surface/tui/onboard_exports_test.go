package tui

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// KnownAgents is the wizard's list: no echo, no opt-in tools.
func TestKnownAgents(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	var ids []string
	for _, a := range KnownAgents(reg) {
		ids = append(ids, a.ID)
	}
	require.NotContains(t, ids, "echo")
	require.NotContains(t, ids, "gemini")
	require.Contains(t, ids, "claude")
	require.Contains(t, ids, "antigravity")
}

func TestAgentCommandAndHint(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	agy, ok := reg.Find("antigravity")
	require.True(t, ok)
	require.Equal(t, "agy", AgentCommand(agy))
	claude, ok := reg.Find("claude")
	require.True(t, ok)
	require.Contains(t, AgentInstallHint(claude), "https://")

	bare := registry.Tool{ID: "x", Command: []string{"xbin"}}
	require.Equal(t, "install xbin and put it on your PATH", AgentInstallHint(bare))
}

func TestBoardKeyDesc(t *testing.T) {
	d, ok := BoardKeyDesc("cc")
	require.True(t, ok)
	require.Equal(t, "mark done / undo", d)
	_, ok = BoardKeyDesc("qq / ctrl+]")
	require.True(t, ok, "attach keys count")
	_, ok = BoardKeyDesc("ctrl+s")
	require.False(t, ok, "a new-agent page key is not a board key")
	_, ok = BoardKeyDesc("nope")
	require.False(t, ok)
}

// A daemon that predates RescanTools rejects it with an Error carrying the
// intent's ID. The rescan swallows it: nothing reaches the board's event
// stream, where an Error would show as a daemon error and drop a pending
// spawn.
func TestRescanTools_OldDaemonErrorStaysOffTheBoard(t *testing.T) {
	require.Nil(t, rescanTools(nil))

	dir, err := os.MkdirTemp("", "rx")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	stop, served := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stop); <-served })
	go func() {
		defer close(served)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		env, err := protocol.NewReader(conn).Read()
		if err != nil {
			return
		}
		_ = protocol.NewWriter(conn).WriteEvent(protocol.EventError, env.ID,
			protocol.ErrorEvent{ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: "intent not implemented"})
		<-stop
	}()

	c, err := client.Dial(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.Nil(t, rescanTools(c)())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = c.NextEventContext(ctx)
	require.Error(t, err, "the rejection went to the Do waiter, not the event stream")
}
