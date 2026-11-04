package pty

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestRedactArgv(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{}},
		{
			"claude prompt after double dash",
			[]string{"claude", "--model", "sonnet", "--", "fix the login bug"},
			[]string{"claude", "--model", "<redacted:6 bytes>", "--", "<redacted:17 bytes>"},
		},
		{
			"short prompt after double dash",
			[]string{"codex", "exec", "--", "hi"},
			[]string{"codex", "exec", "--", "<redacted:2 bytes>"},
		},
		{
			"gemini prompt in flag value",
			[]string{"gemini", "-m", "gemini-2.5-pro", "--prompt-interactive=do X"},
			[]string{"gemini", "-m", "<redacted:14 bytes>", "--prompt-interactive=<redacted:4 bytes>"},
		},
		{
			"short subcommands kept",
			[]string{"ollama", "run", "llama3"},
			[]string{"ollama", "run", "llama3"},
		},
		{
			"long or free-text positionals redacted",
			[]string{"tool", "/Users/alice/secret/project", "two words"},
			[]string{"tool", "<redacted:27 bytes>", "<redacted:9 bytes>"},
		},
		{
			"boolean flag then flag",
			[]string{"claude", "--dangerously-skip-permissions", "--verbose"},
			[]string{"claude", "--dangerously-skip-permissions", "--verbose"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, redactArgv(tc.in))
		})
	}
}

// captureSlog points the default logger at a buffer for the test. Tests in
// this package do not run in parallel, so swapping the global is safe.
func captureSlog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// S-02: the initial prompt rides on argv; it must never reach the log.
func TestSupervisor_LogsRedactedArgv(t *testing.T) {
	const secret = "secret prompt text"
	cases := []struct {
		name string
		argv []string
		line string
	}{
		{"started", []string{"sh", "-c", "exit 0", "--", secret}, "pty: started"},
		{"start failed", []string{"/nonexistent/rex-test-binary", "--", secret}, "pty: start failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureSlog(t)
			store := state.NewStore()
			sess := &state.Session{ID: "argv1", ShortID: "argv1", ToolID: "claude", ModelID: "sonnet",
				Slug: "t", State: protocol.StateQueued, StartedAt: time.Now().UTC()}
			require.NoError(t, store.Add(sess))
			sup := New(SupervisorConfig{StateDir: t.TempDir(), Store: store, Command: tc.argv, IdleTick: 10 * time.Millisecond})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = sup.Run(ctx, sess)

			out := logs.String()
			require.Contains(t, out, tc.line)
			require.Contains(t, out, "tool=claude")
			require.Contains(t, out, "model=sonnet")
			require.Contains(t, out, "argc=")
			require.NotContains(t, out, secret)
			require.NotContains(t, out, "prompt text")
		})
	}
}

// syncBuffer is a bytes.Buffer safe for the concurrent writes slog makes
// from the supervisor's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
