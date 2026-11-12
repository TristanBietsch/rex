package redact

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The zero Policy is what the daemon logs for a child's command line.
func TestArgvZeroPolicy(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{}},
		{"claude prompt after double dash",
			[]string{"claude", "--model", "sonnet", "--", "fix the login bug"},
			[]string{"claude", "--model", "<redacted:6 bytes>", "--", "<redacted:17 bytes>"}},
		{"short prompt after double dash",
			[]string{"codex", "exec", "--", "hi"},
			[]string{"codex", "exec", "--", "<redacted:2 bytes>"}},
		{"gemini prompt in flag value",
			[]string{"gemini", "-m", "gemini-2.5-pro", "--prompt-interactive=do X"},
			[]string{"gemini", "-m", "<redacted:14 bytes>", "--prompt-interactive=<redacted:4 bytes>"}},
		{"short subcommands kept",
			[]string{"ollama", "run", "llama3"},
			[]string{"ollama", "run", "llama3"}},
		{"long or free-text positionals redacted",
			[]string{"tool", "/Users/alice/secret/project", "two words"},
			[]string{"tool", "<redacted:27 bytes>", "<redacted:9 bytes>"}},
		{"boolean flag then flag",
			[]string{"claude", "--dangerously-skip-permissions", "--verbose"},
			[]string{"claude", "--dangerously-skip-permissions", "--verbose"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Argv(tc.in, Policy{}))
		})
	}
}

var cliPolicy = Policy{
	KeepValues: map[string]bool{"tool": true, "model": true, "o": true, "n": true},
}

func TestArgvKeepValues(t *testing.T) {
	require.Equal(t,
		[]string{"rex", "ls", "-tool", "claude", "-o", "json", "-state=<redacted:4 bytes>", "-cwd", "<redacted:9 bytes>"},
		Argv([]string{"rex", "ls", "-tool", "claude", "-o", "json", "-state=done", "-cwd", "/tmp/proj"}, cliPolicy))
	require.Equal(t, []string{"rex", "ls", "--tool=claude", "--model", "opus"},
		Argv([]string{"rex", "ls", "--tool=claude", "--model", "opus"}, cliPolicy))
}

func TestArgvFreeTextAndSelectors(t *testing.T) {
	reply := Policy{Selectors: 1, FreeTextFrom: 3}
	// verb kept; a short hex id kept; a slug that is not hex becomes a size; text redacted.
	require.Equal(t, []string{"rex", "reply", "7d4f", "<redacted:9 bytes>", "<redacted:3 bytes>"},
		Argv([]string{"rex", "reply", "7d4f", "my secret", "yes"}, reply))
	require.Equal(t, []string{"rex", "reply", "<sel:11 bytes>", "<redacted:3 bytes>"},
		Argv([]string{"rex", "reply", "fix-auth-ui", "yes"}, reply))
}

func TestArgvNew(t *testing.T) {
	p := Policy{KeepValues: cliPolicy.KeepValues, FreeTextFrom: 2}
	got := Argv([]string{"rex", "new", "-tool", "claude", "-cwd", "/home/a/b", "--", "fix the bug"}, p)
	require.Equal(t, []string{"rex", "new", "-tool", "claude", "-cwd", "<redacted:9 bytes>", "--", "<redacted:11 bytes>"}, got)
	got = Argv([]string{"rex", "new", "a prompt without dashes"}, p)
	require.Equal(t, []string{"rex", "new", "<redacted:23 bytes>"}, got)
}

func TestArgvNeverKeepsFreeText(t *testing.T) {
	for _, in := range [][]string{
		{"rex", "config", "set", "auto_trust_dirs", "/home/alice/work"},
		{"rex", "name", "7d4f", "private name"},
	} {
		p := Policy{Selectors: 1, FreeTextFrom: 3}
		if in[1] == "config" {
			p = Policy{FreeTextFrom: 4}
		}
		for _, a := range Argv(in, p) {
			require.NotContains(t, a, "alice")
			require.NotContains(t, a, "private")
		}
	}
}
