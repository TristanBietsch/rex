package lifecycle

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
)

func daemonPrinter(t *testing.T, format string, width int, color bool) (*core.Printer, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, OutTTY: color, ErrTTY: color,
		Width: width, Scheme: "default", TrueColor: true, GOOS: "linux"}
	p, err := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, io.Discard)
	require.NoError(t, err)
	return p, &out
}

func TestDaemonViewsGolden(t *testing.T) {
	cases := []struct {
		name   string
		d      daemonData
		text   string
		txt    string
		failed bool
	}{
		{"started", daemonData{Action: "start", State: "started", Socket: "/s/rex.sock", PID: "4242"},
			"rex-daemon started (pid 4242)", "rex-daemon started (pid 4242)", false},
		{"already", daemonData{Action: "start", State: "already_running", Socket: "/s/rex.sock"},
			"rex-daemon already running", "rex-daemon already running", false},
		{"stopped", daemonData{Action: "stop", State: "stopped", Socket: "/s/rex.sock", PID: "4242"},
			"stopped rex-daemon (pid 4242)", "stopped rex-daemon (pid 4242) · socket=/s/rex.sock", false},
		{"running", daemonData{Action: "status", State: "running", Socket: "/s/rex.sock", PID: "4242"},
			"rex-daemon is running (pid 4242)", "running · socket=/s/rex.sock · pid=4242", false},
		{"down", daemonData{Action: "status", State: "not_running", Socket: "/s/rex.sock"},
			"rex-daemon is not running", "not running · socket=/s/rex.sock", true},
	}
	var secs []golden.Section
	for _, c := range cases {
		for _, f := range []string{"text", "txt", "md", "json", "agent"} {
			p, out := daemonPrinter(t, f, 80, false)
			require.NoError(t, emitDaemon(p, c.d, c.text, c.txt, c.failed))
			secs = append(secs, golden.Section{Name: c.name + "." + f, Body: out.String()})
		}
	}
	golden.Check(t, "testdata/golden/daemon.txtar", secs...)
}

// status of a daemon that is down is a failing answer: `fail` in agent mode,
// and the line the command always printed in txt.
func TestDaemonStatusDownAgentTrailer(t *testing.T) {
	p, out := daemonPrinter(t, "agent", 80, false)
	require.NoError(t, emitDaemon(p, daemonData{Action: "status", State: "not_running", Socket: "/s"}, "x", "y", true))
	require.Equal(t, "fail cmd=daemon action=status state=not_running socket=/s\n", out.String())
}
