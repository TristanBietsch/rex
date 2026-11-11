package server

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A session that is still starting (queued, no supervisor registered yet) is
// live as far as Resume is concerned: it acks, and neither truncates the
// hook file nor takes the stop slot.
func TestResume_StartingSessionIsLiveNoOp(t *testing.T) {
	const hookTools = `tools:
  - id: echo
    detect: { kind: hooks, format: claude }
    resume:
      args: ["--resume", "{id}"]
    models:
      - id: hook
        name: "Stays up"
        args: ['sleep 30']
`
	e := startResumeServer(t, hookTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateQueued, "hook", testResumeID)
	sess.EndedAt = time.Time{}
	require.NoError(t, e.store.Add(sess))
	hf, err := state.CreateHookFile(e.dir, "a")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(hf, []byte("needs_input\n"), 0o600))
	conn, w, r := e.dial(t)

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Empty(t, code, msg)
	raw, err := os.ReadFile(hf)
	require.NoError(t, err)
	require.Equal(t, "needs_input\n", string(raw), "hook file untouched")
	require.False(t, e.srv.hasStop("a"), "no slot taken")
	require.Equal(t, protocol.StateQueued, e.state("a"))
}

// The persisted effort is checked against the model's current options, as
// NewSession does, before it can reach argv.
func TestResume_StaleEffortIsBadState(t *testing.T) {
	const effortTools = `tools:
  - id: echo
    resume:
      args: ["--resume", "{id}"]
    models:
      - id: eff
        name: "Has effort"
        args: ['sleep 30']
        effort:
          options: ["low", "high"]
          default: low
          arg_template: "--effort={value}"
`
	e := startResumeServer(t, effortTools, 0)
	bad := exited(func() string { return e.dir }, "bad", protocol.StateDone, "eff", testResumeID)
	bad.Effort = "--dangerously-skip-permissions"
	ok := exited(func() string { return e.dir }, "ok", protocol.StateDone, "eff", testResumeID)
	ok.Effort = "high"
	require.NoError(t, e.store.Add(bad))
	require.NoError(t, e.store.Add(ok))
	conn, w, r := e.dial(t)

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "bad"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Contains(t, msg, "effort")
	require.Equal(t, protocol.StateDone, e.state("bad"))
	require.False(t, e.srv.hasStop("bad"))

	code, msg = call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "ok"})
	require.Empty(t, code, msg)
	require.NoError(t, e.srv.deleteSession("ok", protocol.RemovedByUser))
}
