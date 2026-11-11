package export

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func sampleSession() protocol.SessionSummary {
	return protocol.SessionSummary{
		ID:          "11111111-2222-4333-8444-555555555555",
		ShortID:     "1111",
		ToolID:      "claude",
		ModelID:     "opus",
		Effort:      "max",
		Slug:        "fix-the-widget",
		Name:        "Fix the widget",
		CWD:         "/work/example",
		State:       protocol.StateDone,
		StartedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		EndedAt:     time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC),
		Description: "Fixed the widget: tests pass",
		ResumeID:    "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
	}
}

func sampleFrames() Document {
	return Document{
		Version: Version,
		Session: sampleSession(),
		Source:  SourceFrames,
		Frames: []Frame{
			{At: 120, Text: "> fix the widget\nthinking..."},
			{At: 480, Text: "done: all tests pass\n```\ncode fence inside\n```"},
		},
	}
}

func sampleTurns() Document {
	return Document{
		Version: Version,
		Session: sampleSession(),
		Source:  SourceAgentLog,
		Turns: []Turn{
			{Role: "user", Text: "fix the widget", At: "2026-01-02T03:04:06Z"},
			{Role: "assistant", Text: "On it.\n[tool] Edit widget.go", At: "2026-01-02T03:04:09Z"},
		},
	}
}

func render(t *testing.T, d Document, f Format) string {
	t.Helper()
	b, err := Render(d, f)
	require.NoError(t, err)
	return string(b)
}

func TestRender_goldens(t *testing.T) {
	f, t2 := sampleFrames(), sampleTurns()
	golden.Check(t, "testdata/export.txtar",
		golden.Section{Name: "frames.md", Body: render(t, f, FormatMD)},
		golden.Section{Name: "frames.txt", Body: render(t, f, FormatTXT)},
		golden.Section{Name: "turns.md", Body: render(t, t2, FormatMD)},
		golden.Section{Name: "turns.txt", Body: render(t, t2, FormatTXT)},
	)
}

func TestRender_jsonShape(t *testing.T) {
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(render(t, sampleFrames(), FormatJSON)), &got))
	require.EqualValues(t, 1, got["version"])
	require.Equal(t, "frames", got["source"])
	require.Contains(t, got, "session")
	require.Contains(t, got, "frames")
	require.NotContains(t, got, "turns")
	frame := got["frames"].([]any)[0].(map[string]any)
	require.EqualValues(t, 120, frame["at"])
	require.Equal(t, "> fix the widget\nthinking...", frame["text"])
	require.Equal(t, "claude", got["session"].(map[string]any)["tool_id"])
}

func TestRender_yamlDecodesToSameDocumentAsJSON(t *testing.T) {
	for name, d := range map[string]Document{"frames": sampleFrames(), "turns": sampleTurns()} {
		t.Run(name, func(t *testing.T) {
			var fromJSON Document
			require.NoError(t, json.Unmarshal([]byte(render(t, d, FormatJSON)), &fromJSON))

			var generic any
			require.NoError(t, yaml.Unmarshal([]byte(render(t, d, FormatYAML)), &generic))
			asJSON, err := json.Marshal(generic)
			require.NoError(t, err)
			var fromYAML Document
			require.NoError(t, json.Unmarshal(asJSON, &fromYAML))

			require.Equal(t, fromJSON, fromYAML)
			require.Equal(t, 1, fromYAML.Version)
			require.Equal(t, d.Session.ID, fromYAML.Session.ID)
		})
	}
}

func TestRender_yamlKeepsKeyOrderAndMultilineText(t *testing.T) {
	out := render(t, sampleFrames(), FormatYAML)
	require.True(t, strings.HasPrefix(out, "version: 1\nsession:\n"), out)
	require.Contains(t, out, "text: |")
}

func TestRender_includesNoEnvironment(t *testing.T) {
	t.Setenv("REX_EXPORT_SECRET_PROBE", "hunter2")
	for _, f := range Formats() {
		require.NotContains(t, render(t, sampleFrames(), f), "hunter2")
	}
}

func TestRender_mdAndTxtFlattenMetadata(t *testing.T) {
	d := sampleFrames()
	d.Session.Name = "evil\x1b]0;title\x07\nname | pipe"
	for _, f := range []Format{FormatMD, FormatTXT} {
		out := render(t, d, f)
		require.NotContains(t, out, "\x1b")
		require.NotContains(t, out, "\x07")
	}
	require.Contains(t, render(t, d, FormatMD), `\|`)
}

func TestRender_fenceLongerThanTextFence(t *testing.T) {
	d := Document{Version: Version, Source: SourceFrames, Frames: []Frame{{Text: "````\nx\n````"}}}
	out := render(t, d, FormatMD)
	require.Contains(t, out, "`````text\n````\nx\n````\n`````\n")
}

func TestRender_noteAndDropped(t *testing.T) {
	d := sampleFrames()
	d.Notice = "agent log not found; showing frames"
	d.FramesDropped = 7
	for _, f := range Formats() {
		out := render(t, d, f)
		require.Contains(t, out, "agent log not found", f)
		require.Contains(t, out, "7", f)
	}
}

func TestRender_tooLarge(t *testing.T) {
	d := Document{Version: Version, Source: SourceFrames, Frames: []Frame{{Text: strings.Repeat("x", MaxOutputBytes+1)}}}
	_, err := Render(d, FormatTXT)
	require.ErrorIs(t, err, ErrOutputTooLarge)
}

func TestRender_unknownFormat(t *testing.T) {
	_, err := Render(sampleFrames(), Format("pdf"))
	require.Error(t, err)
}

func TestParseFormat(t *testing.T) {
	for _, f := range Formats() {
		got, err := ParseFormat(string(f))
		require.NoError(t, err)
		require.Equal(t, f, got)
	}
	_, err := ParseFormat("pdf")
	require.ErrorContains(t, err, "json, yaml, md or txt")
}

func TestInferFormat(t *testing.T) {
	cases := map[string]Format{
		"out.json": FormatJSON, "out.JSON": FormatJSON,
		"/a/b/out.yaml": FormatYAML, "out.yml": FormatYAML,
		"out.md": FormatMD, "out.markdown": FormatMD,
		"out.txt": FormatTXT,
	}
	for path, want := range cases {
		got, ok := InferFormat(path)
		require.True(t, ok, path)
		require.Equal(t, want, got, path)
	}
	for _, path := range []string{"out", "out.log", "out.", ".json.bak"} {
		_, ok := InferFormat(path)
		require.False(t, ok, path)
	}
}

func TestFormatExt(t *testing.T) {
	require.Equal(t, "json", FormatJSON.Ext())
	require.Equal(t, "yaml", FormatYAML.Ext())
	require.Equal(t, "md", FormatMD.Ext())
	require.Equal(t, "txt", FormatTXT.Ext())
}
