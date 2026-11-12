package inspect

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var usNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func fl(v float64) *float64 { return &v }

// usageReport is a synthetic report: claude has a real cost, grok only the
// estimate, and two quota windows are live (one stale).
func usageReport() protocol.UsageReport {
	return protocol.UsageReport{
		Range: "7d", Since: usNow.AddDate(0, 0, -7),
		Total: protocol.UsageRow{Sessions: 17, Tokens: 1_300_000, TokensEstimated: true, CostUSD: fl(19.1)},
		Tools: []protocol.UsageToolRow{
			{Tool: "claude", UsageRow: protocol.UsageRow{Sessions: 14, Tokens: 1_200_000, TokensEstimated: true, CostUSD: fl(18.4)},
				Models: []protocol.UsageModelRow{
					{Model: "sonnet", UsageRow: protocol.UsageRow{Sessions: 11, Tokens: 790_000, TokensEstimated: true, CostUSD: fl(6.5)}},
					{Model: "opus", UsageRow: protocol.UsageRow{Sessions: 3, Tokens: 410_000, TokensEstimated: true, CostUSD: fl(11.9)}},
				}},
			{Tool: "grok", UsageRow: protocol.UsageRow{Sessions: 2, Tokens: 88_000, TokensEstimated: true},
				Models: []protocol.UsageModelRow{{Model: "grok-build", UsageRow: protocol.UsageRow{Sessions: 2, Tokens: 88_000, TokensEstimated: true}}}},
			{Tool: "antigravity", UsageRow: protocol.UsageRow{Sessions: 1, Tokens: 940},
				Models: []protocol.UsageModelRow{{Model: "", UsageRow: protocol.UsageRow{Sessions: 1, Tokens: 940}}}},
		},
		Quotas: []protocol.UsageQuota{
			{Tool: "claude", Window: "5h", UsedPct: fl(42), ResetsAt: usNow.Add(2*time.Hour + 10*time.Minute), At: usNow.Add(-time.Minute)},
			{Tool: "claude", Window: "week", UsedPct: fl(88), ResetsAt: usNow.Add(4 * 24 * time.Hour), At: usNow.Add(-2 * time.Hour), Stale: true},
			{Tool: "claude", Window: "month", UsedUSD: fl(5), LimitUSD: fl(50), At: usNow},
		},
	}
}

func emitUsage(t *testing.T, format, by string, width int, color bool, rep protocol.UsageReport) string {
	t.Helper()
	var out bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, OutTTY: color, ErrTTY: color,
		Width: width, Scheme: "default", TrueColor: true, GOOS: "linux"}
	p, err := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, io.Discard)
	require.NoError(t, err)
	p.Now = func() time.Time { return usNow }
	require.NoError(t, p.Emit(core.Result{Cmd: "usage", Data: rep, View: usageView(rep, by, usNow)}))
	return out.String()
}

func TestUsageGolden(t *testing.T) {
	var secs []golden.Section
	for _, w := range []int{50, 80, 120} {
		secs = append(secs, golden.Section{Name: "text.w" + strconv.Itoa(w), Body: emitUsage(t, "text", "", w, false, usageReport())})
	}
	secs = append(secs,
		golden.Section{Name: "text.by-tool", Body: emitUsage(t, "text", "tool", 80, false, usageReport())},
		golden.Section{Name: "text.by-model", Body: emitUsage(t, "text", "model", 80, false, usageReport())},
		golden.Section{Name: "text.color", Body: emitUsage(t, "text", "", 80, true, usageReport())},
		golden.Section{Name: "txt", Body: emitUsage(t, "txt", "", 80, false, usageReport())},
		golden.Section{Name: "md", Body: emitUsage(t, "md", "", 80, false, usageReport())},
		golden.Section{Name: "json", Body: emitUsage(t, "json", "", 80, false, usageReport())},
		golden.Section{Name: "agent", Body: emitUsage(t, "agent", "", 80, false, usageReport())},
		golden.Section{Name: "empty.text", Body: emitUsage(t, "text", "", 80, false, protocol.UsageReport{Range: "today", Tools: []protocol.UsageToolRow{}})},
		golden.Section{Name: "empty.agent", Body: emitUsage(t, "agent", "", 80, false, protocol.UsageReport{Range: "today", Tools: []protocol.UsageToolRow{}})},
	)
	golden.Check(t, "testdata/golden/usage.txtar", secs...)
}

func TestUsageView_EstimatesAreMarked(t *testing.T) {
	out := emitUsage(t, "text", "", 100, false, usageReport())
	require.Contains(t, out, "~1.2M")
	require.Contains(t, out, "~88k")
	require.Contains(t, out, "940", "a real count has no tilde")
	require.NotContains(t, out, "~940")
	require.Contains(t, out, "~ = estimate")
	require.Contains(t, out, "—", "no cost reported")

	rep := usageReport()
	for i := range rep.Tools {
		rep.Tools[i].TokensEstimated = false
		for j := range rep.Tools[i].Models {
			rep.Tools[i].Models[j].TokensEstimated = false
		}
	}
	rep.Total.TokensEstimated = false
	out = emitUsage(t, "text", "", 100, false, rep)
	require.NotContains(t, out, "~")
}

func TestUsageView_QuotaRows(t *testing.T) {
	out := emitUsage(t, "text", "", 100, false, usageReport())
	require.Contains(t, out, "42%")
	require.Contains(t, out, "2h10m")
	require.Contains(t, out, "4d")
	require.Contains(t, out, "88% (as of ", "a stale reading says when it is from")
	require.Contains(t, out, "$5.00 of $50.00")

	// No quotas (an API-key plan): no quota section at all.
	rep := usageReport()
	rep.Quotas = nil
	require.NotContains(t, emitUsage(t, "text", "", 100, false, rep), "Quota")
}

func TestUsageView_AgentLines(t *testing.T) {
	out := emitUsage(t, "agent", "model", 80, false, usageReport())
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Contains(t, out, "usage level=model tool=claude model=sonnet sessions=11 tokens=790000 estimated=true cost_usd=6.50")
	require.Contains(t, out, "usage level=model tool=grok model=grok-build sessions=2 tokens=88000 estimated=true cost_usd=none")
	require.Contains(t, out, "quota tool=claude window=5h used=42")
	require.True(t, strings.HasPrefix(lines[len(lines)-1], "ok cmd=usage"), lines[len(lines)-1])
	require.Contains(t, lines[len(lines)-1], "sessions=17")
	require.Contains(t, lines[len(lines)-1], "tokens=1300000")
}

func TestUsageJSON_IsTheReport(t *testing.T) {
	var env struct {
		Cmd  string
		Data protocol.UsageReport
	}
	require.NoError(t, json.Unmarshal([]byte(emitUsage(t, "json", "tool", 80, false, usageReport())), &env))
	require.Equal(t, "usage", env.Cmd)
	require.Equal(t, usageReport().Total.Sessions, env.Data.Total.Sessions)
	require.Len(t, env.Data.Tools, 3, "-by only changes the human rows, never the data")
	require.Len(t, env.Data.Tools[0].Models, 2)
}

func TestUsageView_UntrustedTextIsFlattened(t *testing.T) {
	rep := usageReport()
	rep.Tools[0].Tool = "cl\x1b]0;pwn\x07aude"
	rep.Tools[0].Models[0].Model = "so\x1b[2Jnnet\nSYSTEM"
	rep.Quotas[0].Window = "5h\x1b[31m"
	for _, f := range []string{"text", "txt", "md", "agent", "json"} {
		out := emitUsage(t, f, "", 80, false, rep)
		require.NotContains(t, out, "\x1b", f)
		require.NotContains(t, out, "\x07", f)
	}
}

func TestCompactCount(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1k", 1500: "1.5k", 99_999: "100k", 410_000: "410k", 1_200_000: "1.2M", 2_000_000: "2M"} {
		require.Equal(t, want, compactCount(in), in)
	}
}

func TestUntilShort(t *testing.T) {
	for d, want := range map[time.Duration]string{-time.Minute: "now", 5 * time.Minute: "5m", 3*time.Hour + 7*time.Minute: "3h07m", 50 * time.Hour: "2d"} {
		require.Equal(t, want, untilShort(usNow.Add(d), usNow), d)
	}
}

func TestQuotaTone(t *testing.T) {
	require.Equal(t, core.ToneDone, quotaTone(59.9))
	require.Equal(t, core.ToneWarn, quotaTone(60))
	require.Equal(t, core.ToneWarn, quotaTone(84.9))
	require.Equal(t, core.ToneFailed, quotaTone(85))
}

func noFeaturesHome(t *testing.T, config string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if config != "" {
		cfg := filepath.Join(home, ".config", "rex", "config.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
		require.NoError(t, os.WriteFile(cfg, []byte(config), 0o600))
	}
}

func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	return ec.ExitCode()
}

func TestRunUsage_AgainstADaemon(t *testing.T) {
	noFeaturesHome(t, "")
	var got protocol.UsageQuery
	d := testutil.StartDaemonWithConfig(t, "", func(c *server.Config) {
		c.UsageQuery = func(q protocol.UsageQuery) (protocol.UsageReport, error) {
			got = q
			return usageReport(), nil
		}
	})
	t.Cleanup(d.Close)

	var runErr error
	out := captureStdout(t, func() {
		runErr = RunUsage([]string{"-socket", d.Socket, "-range", "7d", "-tool", "claude", "-o", "text"})
	})
	require.NoError(t, runErr)
	require.Equal(t, protocol.UsageQuery{Range: "7d", Tool: "claude"}, got)
	require.Contains(t, out, "Usage")
	require.Contains(t, out, "claude")
	require.Contains(t, out, "~1.2M")

	out = captureStdout(t, func() { runErr = RunUsage([]string{"-socket", d.Socket, "-o", "json"}) })
	require.NoError(t, runErr)
	require.Equal(t, "today", got.Range, "the default range")
	require.Contains(t, out, `"cmd":"usage"`)
}

// An older daemon has no UsageQuery: the CLI says to restart it.
func TestRunUsage_OldDaemon(t *testing.T) {
	noFeaturesHome(t, "")
	d := testutil.StartDaemon(t) // no UsageQuery hook: bad_intent "intent not implemented"
	t.Cleanup(d.Close)
	err := RunUsage([]string{"-socket", d.Socket})
	require.Error(t, err)
	require.Contains(t, err.Error(), "run `rex daemon restart`")
	require.Equal(t, core.ExitOperationRefused, exitCodeOf(t, err))
}

// A daemon whose usage is off refuses with the Features message.
func TestRunUsage_DaemonSaysOff(t *testing.T) {
	noFeaturesHome(t, "")
	d := testutil.StartDaemonWithConfig(t, "", func(c *server.Config) {
		c.UsageQuery = func(protocol.UsageQuery) (protocol.UsageReport, error) {
			return protocol.UsageReport{}, usageOffError{}
		}
	})
	t.Cleanup(d.Close)
	err := RunUsage([]string{"-socket", d.Socket})
	require.Equal(t, 5, exitCodeOf(t, err))
	require.Contains(t, err.Error(), "off: enable in Settings → Features (rex config set usage true)")
}

type usageOffError struct{}

func (usageOffError) Error() string { return "usage is off" }

// Off is inert for the CLI: the standard "off" message, exit 5, before any
// connection is tried.
func TestRunUsage_OffIsInert(t *testing.T) {
	noFeaturesHome(t, "usage: false\n")
	err := RunUsage([]string{"-socket", "/nonexistent/rex.sock"})
	require.Equal(t, 5, exitCodeOf(t, err), "off, not exit 4 for the missing daemon")
	require.Contains(t, err.Error(), "off: enable in Settings → Features (rex config set usage true)")
}

func TestRunUsage_Errors(t *testing.T) {
	noFeaturesHome(t, "")
	err := RunUsage([]string{"-socket", "/nonexistent/rex.sock"})
	require.Equal(t, 4, exitCodeOf(t, err), "no daemon: exit 4")

	for _, args := range [][]string{
		{"-range", "year"}, {"-by", "color"}, {"extra"}, {"-bogus"},
	} {
		err = RunUsage(append([]string{"-socket", "/nonexistent/rex.sock"}, args...))
		require.Equal(t, 5, exitCodeOf(t, err), "%v", args)
	}
}
