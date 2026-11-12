package tui

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// A theme file someone else wrote can name itself and put values in places
// that reach the boot log and the status line: none of it may carry an escape.
func TestHostileThemeFilesNeverPutEscapesOnScreen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file names cannot hold control characters")
	}
	m := themeModel(t)
	m.Themes = userThemes(t, map[string]string{
		"a\x1b[2Jb.yaml": "colors: {}",
		"ok.yaml":        `colors: {fleet: ["\u001b[2J"]}`,
	})

	step := themesLoadedStep(themesLoadedMsg{Themes: m.Themes})
	require.Equal(t, stepWarn, step.Status)
	require.NotContains(t, step.Desc, "\x1b")
	line := renderBootLine(bootLine{Name: step.Name, Status: step.Status, Desc: step.Desc})
	require.NotContains(t, line, "\x1b[2J")

	// The renderer itself flattens, whatever a step puts in Desc.
	require.NotContains(t, renderBootLine(bootLine{Name: "x", Desc: "a\x1b[2Jb"}), "\x1b[2J")

	applyLive(&m, "theme", "ok")
	require.Contains(t, m.Err, `theme "ok" not loaded`)
	require.NotContains(t, m.Err, "\x1b")
}
