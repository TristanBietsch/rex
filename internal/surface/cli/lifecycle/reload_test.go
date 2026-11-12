package lifecycle

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// R20: rex reload says what changed and what needs a restart.
func TestReload_PrintsReport(t *testing.T) {
	var out, errOut bytes.Buffer
	r := reloader{out: &out, errOut: &errOut, reload: func(context.Context, string) (protocol.ReloadReport, error) {
		return protocol.ReloadReport{
			Reloaded: []string{"tools.yaml", "config.yaml", "init.lua"},
			Changed:  []string{"theme", "summary_model"},
			Restart:  []string{"summary_model"},
		}, nil
	}}
	require.NoError(t, r.run())
	got := out.String()
	// txt (the test printer) keeps the success line silent, as rex reload
	// always was; what changed is printed.
	require.NotContains(t, got, "reloaded tools.yaml")
	require.Contains(t, got, "changed: theme, summary_model")
	require.Contains(t, got, "needs a daemon restart: summary_model")
}

// A failed layer is printed with its file:line and the command fails.
func TestReload_ErrorsFail(t *testing.T) {
	var out, errOut bytes.Buffer
	r := reloader{out: &out, errOut: &errOut, reload: func(context.Context, string) (protocol.ReloadReport, error) {
		return protocol.ReloadReport{Reloaded: []string{"tools.yaml"}, Errors: []string{"/x/init.lua:2: boom (kept the previous handlers)"}}, nil
	}}
	err := r.run()
	requireExitCode(t, err, 1)
	require.Contains(t, out.String()+errOut.String(), "/x/init.lua:2: boom")
}

func TestReload_NoChange(t *testing.T) {
	var out, errOut bytes.Buffer
	r := reloader{out: &out, errOut: &errOut, reload: func(context.Context, string) (protocol.ReloadReport, error) {
		return protocol.ReloadReport{Reloaded: []string{"tools.yaml", "config.yaml"}}, nil
	}}
	require.NoError(t, r.run())
	require.Empty(t, out.String(), "txt stays silent when nothing changed")
}

// A daemon before R20 only acks: the old one-line answer.
func TestReload_LegacyDaemon(t *testing.T) {
	var out, errOut bytes.Buffer
	r := reloader{out: &out, errOut: &errOut, reload: func(context.Context, string) (protocol.ReloadReport, error) {
		return protocol.ReloadReport{Legacy: true}, nil
	}}
	require.NoError(t, r.run())
	require.Empty(t, out.String())
}

func TestReload_DaemonDown(t *testing.T) {
	var out, errOut bytes.Buffer
	r := reloader{out: &out, errOut: &errOut, reload: func(context.Context, string) (protocol.ReloadReport, error) {
		return protocol.ReloadReport{}, daemonctl.ErrNotRunning
	}}
	requireExitCode(t, r.run(), 4)
}
