package tui

import "testing"

func TestKebabTask(t *testing.T) {
	cases := map[string]string{
		"":                                "",
		"   ":                             "",
		"fix auth bug":                    "fix-auth-bug",
		"Fix Auth Bug!":                   "fix-auth-bug",
		"  hello   world  ":               "hello-world",
		"migrate sqlite to libsql":        "migrate-sqlite-to-libsql",
		"!@#$%^":                          "",
		"abcdefghijklmnopqrstuvwxyz0123456789-extra-tail": "abcdefghijklmnopqrstuvwxyz012345",
		"this is a very long description that runs past the limit": "this-is-a-very-long-description",
	}
	for in, want := range cases {
		got := kebabTask(in)
		if got != want {
			t.Errorf("kebabTask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveAgentSlug(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		got := deriveAgentSlug("claude", "opus", "fix auth bug", nil)
		want := "cc.opus.fix-auth-bug"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("codex gpt-5-codex", func(t *testing.T) {
		got := deriveAgentSlug("codex", "gpt-5-codex", "migrate billing", nil)
		want := "cx.gpt-5-codex.migrate-billing"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("gemini 2.5-pro dots become dashes", func(t *testing.T) {
		got := deriveAgentSlug("gemini", "2.5-pro", "refactor auth", nil)
		want := "gm.2-5-pro.refactor-auth"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty task uses hash", func(t *testing.T) {
		got := deriveAgentSlug("claude", "opus", "", nil)
		// We can't pin the exact hash (pid-dependent), but it must have the right prefix and shape.
		const prefix = "cc.opus."
		if len(got) != len(prefix)+4 {
			t.Errorf("got %q, expected length %d", got, len(prefix)+4)
		}
		if got[:len(prefix)] != prefix {
			t.Errorf("got %q, expected prefix %q", got, prefix)
		}
	})

	t.Run("collision appends -N", func(t *testing.T) {
		existing := []string{"cc.opus.fix-auth-bug", "cc.opus.fix-auth-bug-2"}
		got := deriveAgentSlug("claude", "opus", "fix auth bug", existing)
		want := "cc.opus.fix-auth-bug-3"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("unknown tool falls back to first two chars", func(t *testing.T) {
		got := deriveAgentSlug("xyzzy", "model", "do thing", nil)
		want := "xy.model.do-thing"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestToolShort(t *testing.T) {
	cases := map[string]string{
		"claude":   "cc",
		"codex":    "cx",
		"gemini":   "gm",
		"ollama":   "ol",
		"grok":     "gk",
		"deepseek": "ds",
		"kimi":     "km",
		"echo":     "ec",
		"x":        "x",
		"abc":      "ab",
	}
	for in, want := range cases {
		got := toolShort(in)
		if got != want {
			t.Errorf("toolShort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModelShort(t *testing.T) {
	cases := map[string]string{
		"opus":        "opus",
		"gpt-5":       "gpt-5",
		"gpt-5-codex": "gpt-5-codex",
		"2.5-pro":     "2-5-pro",
		"2.5-flash":   "2-5-flash",
		"llama3.1":    "llama3-1",
		"":            "",
	}
	for in, want := range cases {
		got := modelShort(in)
		if got != want {
			t.Errorf("modelShort(%q) = %q, want %q", in, got, want)
		}
	}
}
