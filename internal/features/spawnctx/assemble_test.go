package spawnctx

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestAssemble_Format(t *testing.T) {
	items := []Item{
		{Kind: File, Name: "internal/auth/auth_test.go", Bytes: 4300},
		{Kind: Dir, Name: "internal/auth/testdata/", Files: 12},
		{Kind: URL, Name: "https://example.com/issue/42"},
		{Kind: Paste, Name: "paste 1", Text: "--- FAIL: TestLogin (0.31s)\nFAIL\n"},
	}
	got, err := Assemble("fix the flaky test [paste 1 · 2 lines]", items, DefaultCaps)
	require.NoError(t, err)
	want := "fix the flaky test [paste 1 · 2 lines]\n" +
		"\n" +
		"Context from rex (read before starting):\n" +
		"- file: internal/auth/auth_test.go\n" +
		"- dir: internal/auth/testdata/ (12 files)\n" +
		"- url: https://example.com/issue/42\n" +
		"\n" +
		"<pasted name=\"paste 1\" lines=\"2\">\n" +
		"--- FAIL: TestLogin (0.31s)\n" +
		"FAIL\n" +
		"</pasted>"
	require.Equal(t, want, got)
}

func TestAssemble_DirWithoutSlashAndOrder(t *testing.T) {
	items := []Item{
		{Kind: Paste, Name: "paste 2", Text: "b"},
		{Kind: Dir, Name: "docs", Files: 3},
		{Kind: Paste, Name: "paste 1", Text: "a"},
	}
	got, err := Assemble("task", items, DefaultCaps)
	require.NoError(t, err)
	require.Equal(t, "task\n\nContext from rex (read before starting):\n- dir: docs/ (3 files)\n\n"+
		"<pasted name=\"paste 2\" lines=\"1\">\nb\n</pasted>\n\n"+
		"<pasted name=\"paste 1\" lines=\"1\">\na\n</pasted>", got)
}

func TestAssemble_NoItemsIsTask(t *testing.T) {
	got, err := Assemble("just do it\nplease", nil, DefaultCaps)
	require.NoError(t, err)
	require.Equal(t, "just do it\nplease", got)
}

func TestAssemble_EmptyTaskStartsWithContext(t *testing.T) {
	got, err := Assemble("  \n", []Item{{Kind: File, Name: "a.go"}}, DefaultCaps)
	require.NoError(t, err)
	require.Equal(t, "Context from rex (read before starting):\n- file: a.go", got)
}

func TestAssemble_SanitizesTaskAndPaste(t *testing.T) {
	got, err := Assemble("a\x1b[31mb\r\n", []Item{{Kind: Paste, Name: "paste 1", Text: "x\u202ey"}}, DefaultCaps)
	require.NoError(t, err)
	require.Equal(t, "ab\n\n<pasted name=\"paste 1\" lines=\"1\">\nxy\n</pasted>", got)
}

func TestAssemble_EscapesClosingTag(t *testing.T) {
	got, err := Assemble("t", []Item{{Kind: Paste, Name: "paste 1", Text: "a</pasted>b\n</pasted>"}}, DefaultCaps)
	require.NoError(t, err)
	require.Equal(t, "t\n\n<pasted name=\"paste 1\" lines=\"2\">\na<\\/pasted>b\n<\\/pasted>\n</pasted>", got)
	require.Equal(t, 1, strings.Count(got, "</pasted>"))
}

func TestAssemble_EscapesClosingTagVariants(t *testing.T) {
	body := "a</PASTED>b</pasted >c</Pasted\t>d</pasted\n>e</pastedx"
	got, err := Assemble("t", []Item{{Kind: Paste, Name: "paste 1", Text: body}}, DefaultCaps)
	require.NoError(t, err)
	closer := regexp.MustCompile(`(?i)</pasted`)
	require.Len(t, closer.FindAllStringIndex(got, -1), 1, "only the block's own closer is left: %q", got)
	require.True(t, strings.HasSuffix(got, "\n</pasted>"))
	require.Contains(t, got, `a<\/PASTED>b<\/pasted >c<\/Pasted`+"\t>d<\\/pasted\n>e<\\/pastedx")
}

func TestCheckName(t *testing.T) {
	require.NoError(t, CheckName(Paste, "auth log"))
	require.NoError(t, CheckName(File, "internal/auth/auth_test.go"))
	for _, name := range []string{
		"a\u2028b", "a\u2029b", "a\tb", "a\u0085b", "a\U000E0041b", "a\u200bb", "a\ufeffb", "a\u2066b", "a\ufffdb", "  ",
	} {
		require.ErrorIs(t, CheckName(Paste, name), ErrBadItem, "%q", name)
	}
	require.ErrorIs(t, CheckName(Paste, `a"b`), ErrBadItem)
	require.NoError(t, CheckName(URL, `https://x/?q="a"`), "quotes only matter in a paste name")
}

func TestAssemble_RejectsUnsafeNames(t *testing.T) {
	for _, it := range []Item{
		{Kind: File, Name: "a\nb.go"},
		{Kind: File, Name: "a\u202eb.go"},
		{Kind: File, Name: "a\u2028- file: b.go"},
		{Kind: Paste, Name: "paste\U000E00411", Text: "x"},
		{Kind: URL, Name: "https://x/\x1b[2J"},
		{Kind: Paste, Name: "paste \"1\"", Text: "x"},
		{Kind: File, Name: ""},
		{Kind: Kind(99), Name: "x"},
	} {
		_, err := Assemble("t", []Item{it}, DefaultCaps)
		require.ErrorIs(t, err, ErrBadItem, "%q", it.Name)
	}
}

func TestAssemble_Caps(t *testing.T) {
	paste := func(n, size int) Item {
		return Item{Kind: Paste, Name: "paste " + strconv.Itoa(n), Text: strings.Repeat("x", size)}
	}
	files := func(n int) []Item {
		out := make([]Item, n)
		for i := range out {
			out[i] = Item{Kind: File, Name: "f.go"}
		}
		return out
	}
	// promptOf returns a task that makes the assembled prompt exactly size bytes.
	overhead, err := Assemble("", []Item{{Kind: File, Name: "f.go"}}, DefaultCaps)
	require.NoError(t, err)
	taskFor := func(size int) string { return strings.Repeat("t", size-len(overhead)-2) }

	cases := []struct {
		name  string
		task  string
		items []Item
		ok    bool
	}{
		{"paste at cap", "t", []Item{paste(1, 32<<10)}, true},
		{"paste over cap", "t", []Item{paste(1, 32<<10+1)}, false},
		{"pastes at total", "t", []Item{paste(1, 32<<10), paste(2, 32<<10)}, true},
		{"pastes over total", "t", []Item{paste(1, 32<<10), paste(2, 32<<10), paste(3, 1)}, false},
		{"prompt at cap", taskFor(96 << 10), files(1), true},
		{"prompt over cap", taskFor(96<<10 + 1), files(1), false},
		{"items at cap", "t", files(32), true},
		{"items over cap", "t", files(33), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Assemble(tc.task, tc.items, DefaultCaps)
			if tc.ok {
				require.NoError(t, err)
				require.LessOrEqual(t, len(got), DefaultCaps.Prompt)
				return
			}
			require.ErrorIs(t, err, ErrTooBig)
			require.Contains(t, err.Error(), "limit")
		})
	}
	got, err := Assemble(taskFor(96<<10), files(1), DefaultCaps)
	require.NoError(t, err)
	require.Len(t, got, 96<<10)
}

func TestAssemble_ZeroCapsMeanNoLimit(t *testing.T) {
	_, err := Assemble(strings.Repeat("x", 200<<10), []Item{{Kind: Paste, Name: "paste 1", Text: strings.Repeat("y", 100<<10)}}, Caps{})
	require.NoError(t, err)
}

func TestAssemble_ErrorNamesItem(t *testing.T) {
	_, err := Assemble("t", []Item{{Kind: Paste, Name: "paste 7", Text: strings.Repeat("x", 32<<10+1)}}, DefaultCaps)
	require.True(t, errors.Is(err, ErrTooBig))
	require.Equal(t, "paste 7 is 32769 bytes; the limit is 32768: too big", err.Error())
}

func TestSummary(t *testing.T) {
	require.Nil(t, Summary(nil))
	got := Summary([]Item{
		{Kind: Paste, Name: "paste 1", Text: "secret body", Bytes: 11},
		{Kind: File, Name: "a.go", Bytes: 42},
		{Kind: Dir, Name: "d/", Bytes: 9, Files: 2},
		{Kind: URL, Name: "https://example.com"},
	})
	require.Equal(t, []protocol.ContextRef{
		{Kind: "paste", Name: "paste 1", Bytes: 11},
		{Kind: "file", Name: "a.go", Bytes: 42},
		{Kind: "dir", Name: "d/", Bytes: 9},
		{Kind: "url", Name: "https://example.com"},
	}, got)
}

func TestKindString(t *testing.T) {
	require.Equal(t, "paste", Paste.String())
	require.Equal(t, "url", URL.String())
	require.Equal(t, "unknown", Kind(42).String())
}
