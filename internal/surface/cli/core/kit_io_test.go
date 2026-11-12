package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithIORedirectsEveryStream(t *testing.T) {
	var out, errw, out2, err2 bytes.Buffer
	env := Env{Getenv: envOf("NO_COLOR", "1"), InTTY: true, ErrTTY: true, Width: 80, Scheme: "default", GOOS: "linux"}
	base, err := NewPrinter(FormatFlags{Output: "text"}, env, &out, &errw)
	require.NoError(t, err)
	p, err := base.WithIO(&out2, &err2, strings.NewReader("y\n"))
	require.NoError(t, err)
	ok, err := p.Confirm("go?", Refusal{Cmd: "x", Flag: "-yes", What: "go"})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "go? [y/N] ", err2.String())
	require.NoError(t, p.Emit(Result{View: View{Line{Text: "done"}}}))
	require.Equal(t, "done\n", out2.String())
	require.Empty(t, out.String()+errw.String())
}

func TestEmitErrIsTextOnly(t *testing.T) {
	for _, f := range []string{"txt", "md", "json", "agent"} {
		var out, errw bytes.Buffer
		p, err := NewPrinter(FormatFlags{Output: f}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, &out, &errw)
		require.NoError(t, err)
		p.EmitErr(View{Line{Text: "plan"}})
		require.Empty(t, out.String()+errw.String(), f)
	}
	var out, errw bytes.Buffer
	p, err := NewPrinter(FormatFlags{Output: "text"}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, &out, &errw)
	require.NoError(t, err)
	p.EmitErr(View{Line{Text: "plan"}})
	require.Equal(t, "plan\n", errw.String())
	require.Empty(t, out.String())
}

// Without an override the log lands in REX_LOG_DIR (a folder the user
// chose is created but not chmodded).
func TestErrLog_DefaultPathFollowsLogDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	t.Setenv("REX_LOG_DIR", dir)
	RecordError([]string{"ls"}, NewExitError(ExitGeneric, "x"), ErrLogOptions{Enabled: func() bool { return true }})
	_, err := os.Stat(filepath.Join(dir, "cli-errors.log"))
	require.NoError(t, err)
}

func TestShrinkRecordFitsTheCap(t *testing.T) {
	rec := ErrLogRecord{V: 1, Cmd: "ls", Chain: []string{strings.Repeat("x", 5000)}}
	b, _ := json.Marshal(rec)
	require.LessOrEqual(t, len(shrinkRecord(b)), errLogMaxRecord)
	require.Len(t, shrinkRecord([]byte(strings.Repeat("z", errLogMaxRecord+100))), errLogMaxRecord)
}
