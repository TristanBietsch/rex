package summarizer

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestClean_StripsSubjectOpeners(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Claude is running the tests", "Running the tests"},
		{"The agent is currently editing main.go", "Editing main.go"},
		{"Agent is waiting for approval", "Waiting for approval"},
		{"It is compiling", "Compiling"},
		{"It's compiling", "Compiling"},
		{"Claude Code has been refactoring auth", "Refactoring auth"},
		{"The AI assistant is now reviewing PR #12", "Reviewing PR #12"},
		{"Currently, Claude is fixing lint", "Fixing lint"},
		{"Codex is running go test ./...", "Running go test ./..."},
		{"I'm reading docs", "Reading docs"},
		{"Claude's editing x.go", "Editing x.go"},
		{"\x1b[1mClaude is\x1b[0m running", "Running"},
		{"Antigravity CLI is still busy fixing the build", "Fixing the build"},
		{"Now fixing the flaky test", "Fixing the flaky test"},
	} {
		require.Equal(t, tc.want, Clean(tc.in, 110, ""), tc.in)
	}
}

func TestClean_StripsToolIDOpener(t *testing.T) {
	require.Equal(t, "Running tests", Clean("Foobar is running tests", 110, "foobar"))
	require.Equal(t, "Foobar is running tests", Clean("Foobar is running tests", 110, ""), "only a known subject is stripped")
}

func TestClean_KeepsNonOpeners(t *testing.T) {
	for _, in := range []string{
		"Claude's config file updated",
		"CLAUDE.md updated",
		"Itemizing invoices",
		"Isolating flaky test",
		"Agentic loop tuned",
		"Waiting: approve git push",
		"Done: tests pass",
	} {
		require.Equal(t, in, Clean(in, 110, ""), in)
	}
}

func TestClean_OpenerOnlyYieldsEmpty(t *testing.T) {
	require.Equal(t, "", Clean("Claude is", 110, ""))
	require.Equal(t, "", Clean("Currently, the agent is", 110, ""))
	require.Equal(t, "", Clean("ok", 110, ""))
}

func TestClean_SentenceCase(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"running tests", "Running tests"},
		{"fixed the lint error", "Fixed the lint error"},
		{"done: tests pass", "Done: tests pass"},
		{"go.mod updated", "go.mod updated"},
		{"pnpm test failing", "pnpm test failing"},
		{"./run.sh finished", "./run.sh finished"},
		{"Running tests", "Running tests"},
		{"éditing x", "Éditing x"},
	} {
		require.Equal(t, tc.want, Clean(tc.in, 110, ""), tc.in)
	}
}

func TestClean_ClampsOnWordBoundary(t *testing.T) {
	long := "Editing the webhook signature verification handlers across the billing service and its tests today"
	for _, max := range []int{60, 110, 180} {
		got := Clean(long+" "+long+" "+long, max, "")
		require.LessOrEqual(t, utf8.RuneCountInString(got), max)
		require.True(t, strings.HasSuffix(got, "…"), got)
	}
	got := Clean("Editing the webhook signature verification handlers across the billing service", 40, "")
	require.Equal(t, "Editing the webhook signature…", got)
	// No space in the last 40%: cut mid-word rather than lose most of the line.
	got = Clean("Verifying "+strings.Repeat("x", 80), 40, "")
	require.Equal(t, 40, utf8.RuneCountInString(got))
	// Trailing separators are trimmed before the ellipsis.
	got = Clean("Editing files, then running tests — and more words follow here", 31, "")
	require.NotContains(t, got, ", …")
	require.NotContains(t, got, "— …")
}

func TestClean_CapsAtProtocolMax(t *testing.T) {
	got := Clean(strings.Repeat("word ", 100), 0, "")
	require.LessOrEqual(t, utf8.RuneCountInString(got), 200)
	got = Clean(strings.Repeat("word ", 100), 5000, "")
	require.LessOrEqual(t, utf8.RuneCountInString(got), 200)
}

func FuzzClean(f *testing.F) {
	for _, s := range []string{
		"Claude is running the tests", "running tests", "\x1b]52;c;ZXZpbA==\x07x", "a\nb",
		"\"\"quoted.\"\"", "- - Output: Output: x y z", "Currently, Claude is Claude is fixing",
		"It's it's it's compiling", strings.Repeat("é", 300), "Claude's editing", "\xff\xfe",
		"Status line: Summary: Running x",
	} {
		f.Add(s, 0)
		f.Add(s, 1)
		f.Add(s, 2)
	}
	caps := []int{60, 110, 180}
	f.Fuzz(func(t *testing.T, in string, i int) {
		max := caps[(i%3+3)%3]
		got := Clean(in, max, "claude")
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8: %q", got)
		}
		if n := utf8.RuneCountInString(got); n > max {
			t.Fatalf("%d runes > %d: %q", n, max, got)
		}
		for _, r := range got {
			if unicode.IsControl(r) {
				t.Fatalf("control rune %U in %q", r, got)
			}
		}
		if again := Clean(got, max, "claude"); again != got {
			t.Fatalf("not idempotent: %q -> %q", got, again)
		}
	})
}
