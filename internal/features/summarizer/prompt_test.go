package summarizer

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestBuildPromptIncludesContext(t *testing.T) {
	p := buildPrompt("codex", "payment-migration", "port billing to the new processor", "running pnpm test:billing\n")
	for _, s := range []string{"codex", "port billing to the new processor", "running pnpm test:billing"} {
		if !contains(p, s) {
			t.Fatalf("prompt missing %q. full:\n%s", s, p)
		}
	}
}

func TestBuildPromptFallsBackToSlugWithoutTask(t *testing.T) {
	p := buildPrompt("codex", "payment-migration", "", "x")
	if !contains(p, "payment-migration") {
		t.Fatalf("prompt missing slug fallback:\n%s", p)
	}
}

func TestCleanResponseStripsArtifacts(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"running pnpm test", "Running pnpm test"},
		{`"running pnpm test"`, "Running pnpm test"},
		{"Output: running pnpm test", "Running pnpm test"},
		{"- running pnpm test", "Running pnpm test"},
		{"running pnpm test.", "Running pnpm test"},
		{"  running pnpm test  ", "Running pnpm test"},
		{"Description: rewriting webhook handlers — 12 of 14", "Rewriting webhook handlers — 12 of 14"},
		{"waiting...", "Waiting..."},
		{"done..", "Done.."},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := cleanResponse(tc.in)
			if got != tc.want {
				t.Fatalf("cleanResponse(%q) = %q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCleanResponseClampsTo60(t *testing.T) {
	long := "this is a description that is definitely longer than sixty characters in total length"
	got := cleanResponse(long)
	if len([]rune(got)) > 60 {
		t.Fatalf("not clamped: len=%d %q", len([]rune(got)), got)
	}
}

// TestCleanResponseIsTerminalSafe: the summary is model output derived from
// the agent's transcript, and every attached TUI prints it verbatim in the
// desc column. It must come out as one line of printable text, with no
// escape sequences or control bytes a transcript could smuggle in (S-03).
func TestCleanResponseIsTerminalSafe(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"cut at first newline", "running tests\nrm -rf /", "Running tests"},
		{"cut at carriage return", "running tests\rfake prompt", "Running tests"},
		{"leading blank lines skipped", "\n\n  running tests\nmore", "Running tests"},
		{"OSC 52 clipboard write", "\x1b]52;c;ZWNobyBwd25lZA==\x07running tests", "Running tests"},
		{"OSC with ST terminator", "\x1b]0;evil title\x1b\\running tests", "Running tests"},
		{"CSI clear screen", "running \x1b[2Jtests", "Running tests"},
		{"SGR color", "\x1b[31mrunning tests\x1b[0m", "Running tests"},
		{"C0 controls dropped", "run\x00ning\x07 tests\x7f", "Running tests"},
		{"tab becomes space", "running\ttests", "Running tests"},
		// U+009B opens a CSI sequence like ESC [ does, and the whole
		// sequence goes (FU-03: the summarizer used to drop only the rune).
		{"C1 CSI rune dropped", "running \u009b31mtests", "Running tests"},
		{"invalid UTF-8 dropped", "running \xff\xfetests", "Running tests"},
		{"prefix after sanitizing", "\x1b[1mOutput:\x1b[0m running tests", "Running tests"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanResponse(tc.in)
			if got != tc.want {
				t.Fatalf("cleanResponse(%q) = %q want %q", tc.in, got, tc.want)
			}
			assertTerminalSafe(t, got)
		})
	}
}

// FuzzCleanResponse: whatever the model returns, the description is one
// line of valid UTF-8 with no control runes, at most maxDescription runes.
func FuzzCleanResponse(f *testing.F) {
	for _, s := range []string{
		"running tests",
		"\x1b]52;c;ZXZpbA==\x07x",
		"a\nb",
		"\x1b[2J\x1b[H",
		"\u009b31m",
		"\xff\xfe",
		"\"quoted.\"",
		strings.Repeat("é", 100),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		assertTerminalSafe(t, cleanResponse(in))
	})
}

func assertTerminalSafe(t *testing.T, s string) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Fatalf("invalid UTF-8: %q", s)
	}
	if n := utf8.RuneCountInString(s); n > maxDescription {
		t.Fatalf("%d runes > %d: %q", n, maxDescription, s)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			t.Fatalf("control rune %U in %q", r, s)
		}
	}
}

// contains is a local helper (avoid importing strings just for tests).
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
