package core

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlatTxtLinesKeepsBreaksButFlattensEachLine(t *testing.T) {
	require.Equal(t, "a\nb", flatTxtLines("a\nb"))
	require.NotContains(t, flatTxtLines("a\x1b]0;pwn\a\nb"), "\x1b")
	require.Contains(t, flatTxtLines("a\x1b]0;pwn\a\nb"), "\n")
	require.Equal(t, "x\ty", flatTxtLines("x\ty"))
}

func TestRunVersionAndAgentHelpEmit(t *testing.T) {
	for f, want := range map[string]string{"txt": Version + "\n", "agent": "ok cmd=version version=" + Version + "\n"} {
		var out bytes.Buffer
		p, err := NewPrinter(FormatFlags{Output: f}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, &out, io.Discard)
		require.NoError(t, err)
		SetOut(p)
		require.NoError(t, RunVersion(nil), f)
		require.Equal(t, want, out.String(), f)
	}
	var out bytes.Buffer
	p, err := NewPrinter(FormatFlags{Output: "txt"}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, &out, io.Discard)
	require.NoError(t, err)
	SetOut(p)
	require.NoError(t, RunAgentHelp(nil))
	require.Contains(t, out.String(), "rex agent card v1")
	require.Error(t, RunVersion([]string{"-bogus"}))
	SetOut(nil)
}

func TestStylesToneMapsEveryTone(t *testing.T) {
	var out bytes.Buffer
	p, err := NewPrinter(FormatFlags{Output: "text"},
		Env{Getenv: envOf(), OutTTY: true, Width: 80, Scheme: "default", TrueColor: true, GOOS: "linux"}, &out, io.Discard)
	require.NoError(t, err)
	for tone := ToneNone; tone <= ToneCrashed; tone++ {
		require.Contains(t, p.Styles.Tone(tone).Render("x"), "x", tone)
	}
}
