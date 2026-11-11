package pty

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/termtext"
)

// Replay harness: feeds a recorded transcript.log through the same virtual
// screen and termtext pipeline the supervisor uses, and logs the text the
// summarizer would be given. Local diagnostics only: the output is never
// asserted on, and recordings must not be committed (fixtures stay synthetic).
//
//	go test ./internal/daemon/pty -run TestReplay -v \
//	    -replay=<session dir>/transcript.log -replay-cols=120 -replay-rows=40
var (
	replayPath = flag.String("replay", "", "transcript.log to replay through the screen pipeline (diagnostics)")
	replayCols = flag.Int("replay-cols", 120, "replay terminal columns")
	replayRows = flag.Int("replay-rows", 40, "replay terminal rows")
)

// replayModelInput returns the summarizer input derived from raw PTY bytes
// on a cols x rows screen: ScreenLines joined and tail-capped as in
// Supervisor.refreshScreenText.
func replayModelInput(t *testing.T, raw []byte, cols, rows int) (screen string, lines []string, input string) {
	t.Helper()
	term := newScreen(t, cols, rows, string(raw))
	screen = term.String()
	lines = termtext.ScreenLines(screen)
	input = termtext.Tail([]byte(strings.Join(lines, "\n")), screenTailBytes)
	return screen, lines, input
}

func TestReplay(t *testing.T) {
	if *replayPath == "" {
		t.Skip("set -replay=PATH to replay a transcript")
	}
	raw, err := os.ReadFile(*replayPath)
	require.NoError(t, err)
	screen, lines, input := replayModelInput(t, raw, *replayCols, *replayRows)
	t.Logf("--- raw screen ---\n%s", screen)
	t.Logf("--- readable lines (%d) ---\n%s", len(lines), strings.Join(lines, "\n"))
	t.Logf("--- model input (%d bytes) ---\n%s", len(input), input)
}

func TestReplayModelInput_Synthetic(t *testing.T) {
	raw := []byte("Editing main.go\r\nrunning go test ./...\r\n❯ draft text\r\n? for shortcuts\r\n")
	_, lines, input := replayModelInput(t, raw, 60, 10)
	require.Equal(t, []string{"Editing main.go", "running go test ./..."}, lines)
	require.Equal(t, "Editing main.go\nrunning go test ./...", input)
}
