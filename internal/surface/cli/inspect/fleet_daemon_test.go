package inspect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fleet set waits for the daemon's answer: a rejected fleet is an error,
// not a success line printed while the daemon said no.
func TestFleetSet_ReportsDaemonRejection(t *testing.T) {
	cwd := t.TempDir()
	d := testutil.StartDaemon(t)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("fleet-test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "fleety", CWD: cwd})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))

	var out bytes.Buffer
	p, err := core.NewPrinter(core.FormatFlags{Output: "text"},
		core.Env{Getenv: func(string) string { return "" }, Width: 120, GOOS: runtime.GOOS}, &out, io.Discard)
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })

	// Only control characters: the daemon rejects it (no displayable text).
	err = RunFleet([]string{"--socket", d.Socket, "set", "fleety", "\x01\x02"})
	require.Error(t, err)
	var ec core.ExitCoder
	require.True(t, errors.As(err, &ec))
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
	require.NotContains(t, out.String(), "set fleet")

	out.Reset()
	require.NoError(t, RunFleet([]string{"--socket", d.Socket, "set", "fleety", "blue"}))
	require.Contains(t, out.String(), "set fleet")
}
