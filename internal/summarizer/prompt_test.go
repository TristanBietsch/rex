package summarizer

import "testing"

func TestBuildPromptIncludesContext(t *testing.T) {
	p := buildPrompt("codex", "payment-migration", "running pnpm test:billing\n")
	for _, s := range []string{"codex", "payment-migration", "running pnpm test:billing"} {
		if !contains(p, s) {
			t.Fatalf("prompt missing %q. full:\n%s", s, p)
		}
	}
}

func TestCleanResponseStripsArtifacts(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"running pnpm test", "running pnpm test"},
		{`"running pnpm test"`, "running pnpm test"},
		{"Output: running pnpm test", "running pnpm test"},
		{"- running pnpm test", "running pnpm test"},
		{"running pnpm test.", "running pnpm test"},
		{"  running pnpm test  ", "running pnpm test"},
		{"Description: rewriting webhook handlers — 12 of 14", "rewriting webhook handlers — 12 of 14"},
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
