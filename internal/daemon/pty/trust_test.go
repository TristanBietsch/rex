package pty

import (
	"context"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// testTrust matches the fake dialog below, worded like Claude Code's.
func testTrust() *TrustAnswer {
	return &TrustAnswer{
		Shown:    regexp.MustCompile(`(?s)Accessing workspace:.*Yes, I trust this folder`),
		Selected: regexp.MustCompile(`❯[ \t]*Yes, I trust this folder`),
		Move:     []byte("\x1b[B"),
		Confirm:  []byte("\r"),
	}
}

const (
	dialogNo  = "Accessing workspace:\n❯ No, exit\n  Yes, I trust this folder\n"
	dialogYes = "Accessing workspace:\n  No, exit\n❯ Yes, I trust this folder\n"
)

func TestTrustAnswererMovesThenConfirms(t *testing.T) {
	t0 := time.Unix(1000, 0)
	ta := newTrustAnswerer(testTrust(), t0)

	keys, _ := ta.next("loading…", t0.Add(time.Second))
	require.Nil(t, keys, "no dialog on screen")

	keys, action := ta.next(dialogNo, t0.Add(2*time.Second))
	require.Equal(t, "\x1b[B", string(keys))
	require.Equal(t, "move", action)

	keys, _ = ta.next(dialogNo, t0.Add(2*time.Second+100*time.Millisecond))
	require.Nil(t, keys, "waits trustStep for the redraw")

	keys, action = ta.next(dialogYes, t0.Add(3*time.Second))
	require.Equal(t, "\r", string(keys))
	require.Equal(t, "confirm", action)

	keys, _ = ta.next(dialogYes, t0.Add(4*time.Second))
	require.Nil(t, keys, "done after confirming")
}

func TestTrustAnswererGivesUp(t *testing.T) {
	t0 := time.Unix(1000, 0)
	ta := newTrustAnswerer(testTrust(), t0)
	now := t0
	moves := 0
	for range 10 {
		now = now.Add(trustStep)
		if keys, _ := ta.next(dialogNo, now); keys != nil {
			require.Equal(t, "\x1b[B", string(keys), "never confirms an unselected accept option")
			moves++
		}
	}
	require.Equal(t, trustMaxMoves, moves)
	require.True(t, ta.done)

	late := newTrustAnswerer(testTrust(), t0)
	keys, action := late.next(dialogYes, t0.Add(trustWindow+time.Second))
	require.Nil(t, keys, "a dialog after the window is left to the user")
	require.Equal(t, "window passed", action)
}

func TestTrustAnswererNilIsInert(t *testing.T) {
	require.Nil(t, newTrustAnswerer(nil, time.Now()))
	var ta *trustAnswerer
	keys, _ := ta.next(dialogYes, time.Now())
	require.Nil(t, keys)
	require.Nil(t, newTrustAnswerer(&TrustAnswer{Shown: regexp.MustCompile("x")}, time.Now()), "incomplete answer")
}

// fakeTrustDialog draws a trust dialog with "No, exit" selected, moves the
// selection on Down, and on Enter prints TRUSTED or EXITED and exits.
const fakeTrustDialog = `
sel=0
draw() {
  printf '\033[2J\033[H'
  printf 'Accessing workspace:\r\n'
  if [ $sel = 0 ]; then printf '\342\235\257 No, exit\r\n  Yes, I trust this folder\r\n'
  else printf '  No, exit\r\n\342\235\257 Yes, I trust this folder\r\n'; fi
}
stty raw -echo
draw
while IFS= read -rsn1 k; do
  case "$k" in
    $'\e') read -rsn2 rest; if [ "$rest" = "[B" ]; then sel=1; draw; fi ;;
    $'\r'|'') if [ $sel = 1 ]; then printf 'TRUSTED\r\n'; else printf 'EXITED\r\n'; fi; exit 0 ;;
  esac
done
`

func runFakeDialog(t *testing.T, trust *TrustAnswer, limit time.Duration) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake dialog needs bash and stty")
	}
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "trust1", ShortID: "trust1", ToolID: "fake", Slug: "trust",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:     []string{"bash", "-c", fakeTrustDialog},
		IdleTick:    50 * time.Millisecond,
		TrustPrompt: trust,
	})
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	_ = sup.Run(ctx, sess)
	tail, err := state.TranscriptTail(stateDir, sess.ID, 8192)
	require.NoError(t, err)
	return string(tail)
}

func TestSupervisor_AcceptsTrustDialog(t *testing.T) {
	out := runFakeDialog(t, testTrust(), 4*time.Second) // exits on its own once answered
	require.Contains(t, out, "TRUSTED")
	require.NotContains(t, out, "EXITED", "a bare Enter would have picked \"No, exit\"")
}

func TestSupervisor_LeavesTrustDialogWithoutAnswer(t *testing.T) {
	out := runFakeDialog(t, nil, 1500*time.Millisecond)
	require.Contains(t, out, "Accessing workspace:", "the fake must have drawn the dialog")
	require.False(t, strings.Contains(out, "TRUSTED") || strings.Contains(out, "EXITED"), "no keys are sent: %q", out)
}
