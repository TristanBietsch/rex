package core

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func actionOut(t *testing.T, format string, v View) string {
	t.Helper()
	var out bytes.Buffer
	env := Env{Getenv: func(string) string { return "" }, Width: 80, Scheme: "default", GOOS: "linux"}
	p, err := NewPrinter(FormatFlags{Output: format}, env, &out, io.Discard)
	require.NoError(t, err)
	require.NoError(t, p.Emit(Result{Cmd: "act", View: v}))
	return out.String()
}

// The fields belong to the agent trailer only; the other formats print the
// line alone, with no blank line left by the empty block.
func TestActionView(t *testing.T) {
	v := ActionView(Line{Text: "did it", Txt: "it: done"}, Field{Key: "id", Val: "7d4f"}, Field{Key: "n", Val: 2})
	require.Equal(t, "did it\n", actionOut(t, "text", v))
	require.Equal(t, "it: done\n", actionOut(t, "txt", v))
	require.Equal(t, "did it\n", actionOut(t, "md", v))
	require.Equal(t, "ok cmd=act id=7d4f n=2\n", actionOut(t, "agent", v))
	require.Len(t, ActionView(Line{Text: "x"}), 1)
}

func TestOffError(t *testing.T) {
	err := OffError("resume")
	require.Equal(t, ExitInvalidArgs, err.Code)
	require.Equal(t, "off: enable in Settings → Features (rex config set resume true)", err.Error())
	require.Equal(t, "rex config set resume true", err.Hint)
}
