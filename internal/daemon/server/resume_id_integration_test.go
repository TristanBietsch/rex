package server_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// resumeToolsYAML overlays the builtin echo tool with a resume block and two
// fake agents: "argv" prints the arguments rex gave it, "capture" prints its
// conversation id on exit.
const resumeToolsYAML = `tools:
  - id: echo
    resume:
      args: ["--resume", "{id}"]
      new_args: ["--session-id", "{id}"]
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 0.2']
`

const captureToolsYAML = `tools:
  - id: echo
    resume:
      args: ["--resume={id}"]
      capture_regex: "resume with id=([0-9a-fA-F-]{36})"
    models:
      - id: bye
        name: "Prints its id on exit"
        args: ['echo working; sleep 0.2; echo "resume with id=7D4F3C8A-1234-4ABC-89AB-CDEFABCDEF01"']
`

// runToDone starts one session and returns its SessionAdded summary and the
// patches seen up to and including the one that reaches done and, when
// wantResumePatch, the resume_id patch that follows it.
func runToDone(t *testing.T, toolsYAML, modelID string, wantResumePatch bool) (protocol.SessionSummary, []map[string]any, *testutil.Daemon) {
	t.Helper()
	d := testutil.StartDaemonWithTools(t, toolsYAML)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{
		ToolID: "echo", ModelID: modelID, Slug: "rid", CWD: d.StateDir, // not t.TempDir: Windows removes it before d.Close
	})
	require.NoError(t, err)

	var (
		sum     protocol.SessionSummary
		patches []map[string]any
		gotDone bool
	)
	for {
		env, err := c.NextEventContext(ctx)
		require.NoError(t, err, "timed out; patches so far: %v", patches)
		switch env.Type {
		case protocol.EventSessionAdded:
			require.NoError(t, json.Unmarshal(env.Data, &sum))
		case protocol.EventSessionUpdated:
			var up protocol.SessionUpdated
			require.NoError(t, json.Unmarshal(env.Data, &up))
			patches = append(patches, up.Patch)
			if s, _ := up.Patch["state"].(string); s == string(protocol.StateDone) {
				gotDone = true
			}
			if _, ok := up.Patch["resume_id"]; ok && gotDone {
				return sum, patches, d
			}
			if gotDone && !wantResumePatch {
				return sum, patches, d
			}
		}
	}
}

func readTranscript(t *testing.T, d *testutil.Daemon, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(d.StateDir, "sessions", id, "transcript.log"))
	require.NoError(t, err)
	return string(raw)
}

func readMeta(t *testing.T, d *testutil.Daemon, id string) protocol.SessionSummary {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(d.StateDir, "sessions", id, "meta.json"))
	require.NoError(t, err)
	var m protocol.SessionSummary
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

// A tool with new_args gets a fresh UUID at spawn: SessionAdded already
// carries it, the child was started with --session-id <id>, and meta.json
// keeps it.
func TestNewSession_PreassignsResumeID(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	sum, _, d := runToDone(t, resumeToolsYAML, "argv", false)
	require.True(t, protocol.ValidResumeID(sum.ResumeID), "resume_id %q", sum.ResumeID)
	require.NotEqual(t, sum.ID, sum.ResumeID, "not the rex session id")
	require.Contains(t, readTranscript(t, d, sum.ID), "ARGV: --session-id "+sum.ResumeID)
	require.Equal(t, sum.ResumeID, readMeta(t, d, sum.ID).ResumeID)
}

// A tool without a resume block (the builtin echo) has no resume id
// and gets no extra arguments.
func TestNewSession_NoResumeBlockNoID(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	sum, patches, _ := runToDone(t, "", "short", false)
	require.Empty(t, sum.ResumeID)
	for _, p := range patches {
		require.NotContains(t, p, "resume_id")
	}
}

// capture_regex: the id the tool prints on exit is lowercased, stored before
// the final meta.json write, and announced by a patch after the terminal one.
func TestNewSession_CapturesResumeIDAtExit(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	sum, patches, d := runToDone(t, captureToolsYAML, "bye", true)
	require.Empty(t, sum.ResumeID, "no new_args: unknown until the child exits")
	last := patches[len(patches)-1]
	require.Equal(t, "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", last["resume_id"])
	require.Equal(t, "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", readMeta(t, d, sum.ID).ResumeID)
	require.Equal(t, protocol.StateDone, readMeta(t, d, sum.ID).State)
}

// A hook-detected agent reports its conversation id with a sid line while it
// runs (what Claude's SessionStart hook writes). The daemon patches it, and a
// later one (a /clear or in-app /resume) replaces it.
const hookToolsYAML = `tools:
  - id: echo
    detect: { kind: hooks, format: claude }
    resume:
      args: ["--resume", "{id}"]
    models:
      - id: sid
        name: "Reports sids through the hook file"
        args: ['printf "needs_input\nsid 7d4f3c8a-1234-4abc-89ab-cdefabcdef01\n" >> "$REX_HOOK_FILE"; sleep 0.6; echo "sid 11111111-2222-4333-8444-555555555555" >> "$REX_HOOK_FILE"; sleep 0.6']
`

func TestNewSession_HookReportsResumeID(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	d := testutil.StartDaemonWithTools(t, hookToolsYAML)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{ToolID: "echo", ModelID: "sid", Slug: "hook", CWD: d.StateDir})
	require.NoError(t, err)

	var got []string
	for len(got) < 2 {
		env, err := c.NextEventContext(ctx)
		require.NoError(t, err, "patches so far: %v", got)
		if env.Type != protocol.EventSessionUpdated {
			continue
		}
		var up protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &up))
		if v, ok := up.Patch["resume_id"].(string); ok {
			got = append(got, v)
		}
	}
	require.Equal(t, []string{"7d4f3c8a-1234-4abc-89ab-cdefabcdef01", "11111111-2222-4333-8444-555555555555"}, got)
}
