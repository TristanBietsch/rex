package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestRun_RefusedSummarizerEndpoint runs the daemon with the summarizer on and
// OLLAMA_HOST pointing off this machine without summary_allow_remote: the
// daemon still runs, no summarizer worker starts (so the only SummarizerHealth
// a client can see is the refusal, never a probe result), and that health
// names the reason without repeating OLLAMA_HOST.
func TestRun_RefusedSummarizerEndpoint(t *testing.T) {
	old := summaryRefusedResend
	summaryRefusedResend = 50 * time.Millisecond
	t.Cleanup(func() { summaryRefusedResend = old })
	t.Setenv("OLLAMA_HOST", "https://gpu.example:11434/v1x")

	cfgPath, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o700))
	require.NoError(t, os.WriteFile(cfgPath, []byte("summary_enabled: true\n"), 0o600))

	dir, err := os.MkdirTemp("", "rexd")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	errc := make(chan error, 1)
	go func() { errc <- run([]string{"-socket", sock, "-state-dir", dir, "-tools", ""}) }()

	var c *client.Client
	require.Eventually(t, func() bool {
		var derr error
		c, derr = client.Dial(sock)
		return derr == nil
	}, 10*time.Second, 20*time.Millisecond)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello(protocol.ClientTUI)
	require.NoError(t, err)

	deadline := time.Now().Add(10 * time.Second)
	var got protocol.SummarizerHealth
	for {
		require.True(t, time.Now().Before(deadline), "no SummarizerHealth within 10s")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		env, err := c.NextEventContext(ctx)
		cancel()
		require.NoError(t, err)
		if env.Type == protocol.EventSummarizerHealth {
			require.NoError(t, json.Unmarshal(env.Data, &got))
			break
		}
	}
	require.False(t, got.Available)
	require.Contains(t, got.Reason, "not local")
	require.NotContains(t, got.Reason, "11434")
	require.NotContains(t, got.Reason, "v1x")
	require.NotContains(t, got.Reason, "https://")

	require.NoError(t, c.Shutdown())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop after Shutdown")
	}
}
