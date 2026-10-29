package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
)

func writeDaemonLog(t *testing.T, content string) {
	t.Helper()
	isolateHome(t)
	path := daemonctl.LogPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// tailDaemonLog used to scan the whole log line by line (and gave up on a
// line over 1 MB, showing nothing). It now reads only the final window.
func TestTailDaemonLog_ReadsOnlyTheTail(t *testing.T) {
	var b strings.Builder
	b.WriteString(strings.Repeat("x", 2<<20) + "\n") // one 2 MB line
	for i := range 100 {
		fmt.Fprintf(&b, "line %03d\n", i)
	}
	writeDaemonLog(t, b.String())

	got := tailDaemonLog(60)
	require.Len(t, got, 60)
	require.Equal(t, "line 040", got[0])
	require.Equal(t, "line 099", got[59])
}

func TestTailDaemonLog_DropsPartialFirstLine(t *testing.T) {
	line := strings.Repeat("y", 1000)
	var b strings.Builder
	for range 100 { // ~100 KB, larger than the read window
		b.WriteString(line + "\r\n")
	}
	writeDaemonLog(t, b.String())

	got := tailDaemonLog(1000)
	require.NotEmpty(t, got)
	require.Less(t, len(got), 100)
	for _, ln := range got {
		require.Equal(t, line, ln, "no fragment, no trailing \\r")
	}
}

func TestTailDaemonLog_ShortAndMissing(t *testing.T) {
	writeDaemonLog(t, "a\nb\n")
	require.Equal(t, []string{"a", "b"}, tailDaemonLog(60))

	writeDaemonLog(t, "")
	require.Empty(t, tailDaemonLog(60))

	isolateHome(t)
	got := tailDaemonLog(60)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "no log at")
}

// openFail reads daemon.log in a Cmd, not in Update; the result fills only
// the popup it was read for.
func TestOpenFail_ReadsLogOffTheEventLoop(t *testing.T) {
	writeDaemonLog(t, "a\nb\n")
	m, cmd := openFail(Model{Focus: FocusBoard})
	require.Equal(t, FocusFail, m.Focus)
	require.Nil(t, m.Fail.LogLines, "no file I/O in Update")
	require.Contains(t, ansi.Strip(renderFail(m)), "(reading daemon.log…)")
	require.NotNil(t, cmd)
	msg := cmd()

	next, _ := m.Update(msg)
	got := next.(Model)
	require.Equal(t, []string{"a", "b"}, got.Fail.LogLines)
	require.NotContains(t, ansi.Strip(renderFail(got)), "reading daemon.log")

	// Closed, or closed and reopened, before the read finished: dropped.
	next, _ = closeFail(m).Update(msg)
	require.Nil(t, next.(Model).Fail)
	reopened, _ := openFail(closeFail(m))
	next, _ = reopened.Update(msg)
	require.Nil(t, next.(Model).Fail.LogLines)
}
