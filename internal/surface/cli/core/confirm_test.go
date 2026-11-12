package core

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func confirmPrinter(t *testing.T, format string, in, errTTY bool, stdin string) (*Printer, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errw bytes.Buffer
	env := Env{Getenv: envOf("NO_COLOR", "1"), InTTY: in, ErrTTY: errTTY, Width: 80, Scheme: "default", GOOS: "linux"}
	p, err := NewPrinter(FormatFlags{Output: format}, env, &out, &errw)
	require.NoError(t, err)
	p.In = strings.NewReader(stdin)
	return p, &out, &errw
}

var rmRefusal = Refusal{Cmd: "rm", Flag: "-force", What: "delete", Hint: "rex rm x -force", Extra: "or -dry-run"}

func TestConfirmAsksOnStderr(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{{"y\n", true}, {"YES\n", true}, {"n\n", false}, {"\n", false}, {"", false}} {
		p, out, errw := confirmPrinter(t, "text", true, true, tc.in)
		ok, err := p.Confirm("delete 7d4f?", rmRefusal)
		require.NoError(t, err)
		require.Equal(t, tc.want, ok, "answer %q", tc.in)
		require.Equal(t, "delete 7d4f? [y/N] ", errw.String(), "the prompt is on stderr")
		require.Empty(t, out.String(), "never on stdout")
	}
}

// Without both terminals, or in any format but text, nothing is asked and
// the command is refused with exit 6 and a hint.
func TestConfirmRefusesWhenItCannotAsk(t *testing.T) {
	for _, tc := range []struct {
		name         string
		format       string
		in, errTTY   bool
		wantInReason string
	}{
		{"stdin pipe", "text", false, true, "stdin is not a terminal"},
		{"stderr pipe", "text", true, false, "stderr is not a terminal"},
		{"no terminals", "txt", false, false, "stdin is not a terminal"},
		{"agent on a terminal", "agent", true, true, "cannot ask in agent mode"},
		{"json on a terminal", "json", true, true, "cannot ask in json mode"},
	} {
		p, _, errw := confirmPrinter(t, tc.format, tc.in, tc.errTTY, "y\n")
		ok, err := p.Confirm("delete?", rmRefusal)
		require.False(t, ok, tc.name)
		var ee ExitError
		require.True(t, errors.As(err, &ee), tc.name)
		require.Equal(t, ExitOperationRefused, ee.Code, tc.name)
		require.Equal(t, "rex rm x -force", ee.Hint, tc.name)
		require.Contains(t, ee.Msg, tc.wantInReason, tc.name)
		require.Contains(t, ee.Msg, "pass -force to delete without asking, or -dry-run", tc.name)
		require.Empty(t, errw.String(), tc.name+": no prompt")
	}
}

func TestWarnAndProgress(t *testing.T) {
	for _, tc := range []struct {
		format, want string
	}{
		{"txt", "rex: kept 5555\n"},
		{"md", "rex: kept 5555\n"},
		{"text", "! kept 5555\n"},
		{"agent", "warn cmd=prune msg=\"kept 5555\"\n"},
		{"json", `{"v":1,"warn":{"cmd":"prune","msg":"kept 5555"}}` + "\n"},
	} {
		p, out, errw := confirmPrinter(t, tc.format, false, false, "")
		p.Cmd = "prune"
		p.Warn("kept 5555")
		require.Equal(t, tc.want, errw.String(), tc.format)
		require.Empty(t, out.String(), tc.format)
	}
	for _, f := range []string{"txt", "md", "json", "agent"} {
		p, _, errw := confirmPrinter(t, f, false, false, "")
		p.Progress("working")
		require.Empty(t, errw.String(), f+": progress is text only")
	}
	p, _, errw := confirmPrinter(t, "text", false, false, "")
	p.Progress("working")
	require.Equal(t, "· working\n", errw.String())

	p, _, errw = confirmPrinter(t, "txt", false, false, "")
	p.Warn("x\x1b]0;pwn\a y\nz")
	require.NotContains(t, errw.String(), "\x1b")
	require.Equal(t, 1, strings.Count(errw.String(), "\n"), "a warning is one line")
}
