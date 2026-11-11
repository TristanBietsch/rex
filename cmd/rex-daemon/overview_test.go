package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestRun_OverviewEndToEnd runs the whole daemon with overview_enabled on and
// a fake Ollama: a TUI that connects gets an Overview event carrying the
// fake's headline, and the model saw a <sessions> digest of the board.
func TestRun_OverviewEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = io.WriteString(w, `{"models":[{"name":"gemma2:2b"}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Prompt string `json:"prompt"`
		}
		_ = json.Unmarshal(b, &req)
		mu.Lock()
		prompts = append(prompts, req.Prompt)
		mu.Unlock()
		if strings.Contains(req.Prompt, "<sessions>") {
			_, _ = io.WriteString(w, `{"response":"Review billing-tests now"}`)
			return
		}
		_, _ = io.WriteString(w, `{"response":"Running tests"}`)
	}))
	defer fake.Close()
	t.Setenv("OLLAMA_HOST", fake.URL)

	cfgPath, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o700))
	require.NoError(t, os.WriteFile(cfgPath, []byte("summary_enabled: true\noverview_enabled: true\n"), 0o600))

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

	// A finished session is ready for review; a viewer then asks for the overview.
	_, err = c.Do(context.Background(), protocol.IntentNewSession, protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "billing-tests", CWD: dir, Title: "fix invoice rounding",
	})
	require.NoError(t, err)
	tui, err := client.Dial(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tui.Close() })
	_, err = tui.Hello(protocol.ClientTUI)
	require.NoError(t, err)

	deadline := time.Now().Add(20 * time.Second)
	var got protocol.Overview
	for got.Headline == "" {
		require.True(t, time.Now().Before(deadline), "no Overview with a headline within 20s")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		env, err := tui.NextEventContext(ctx)
		cancel()
		require.NoError(t, err)
		if env.Type == protocol.EventOverview {
			require.NoError(t, json.Unmarshal(env.Data, &got))
		}
	}
	require.True(t, got.Enabled)
	require.Equal(t, "Review billing-tests now", got.Headline)
	require.False(t, got.GeneratedAt.IsZero())

	mu.Lock()
	var digest string
	for _, p := range prompts {
		if strings.Contains(p, "<sessions>") {
			digest = p
		}
	}
	mu.Unlock()
	require.Contains(t, digest, "billing-tests")
	require.Contains(t, digest, "fix invoice rounding")

	require.NoError(t, tui.Shutdown())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop after Shutdown")
	}
}
