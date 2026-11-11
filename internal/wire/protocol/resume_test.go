package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidResumeID(t *testing.T) {
	const lower = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	for _, ok := range []string{lower, strings.ToUpper(lower), "7D4f3c8a-1234-4ABC-89ab-cdefabcdef01"} {
		require.True(t, ValidResumeID(ok), ok)
	}
	for name, bad := range map[string]string{
		"empty":        "",
		"flag":         "-x",
		"flag uuid":    "-" + lower[1:],
		"space":        lower[:8] + " " + lower[9:],
		"too long":     lower + "0",
		"37 chars":     "0" + lower,
		"short":        lower[:35],
		"nul":          lower[:35] + "\x00",
		"non-ascii":    lower[:35] + "é",
		"non-hex":      "7d4f3c8a-1234-4abc-89ab-cdefabcdefzz",
		"no dashes":    strings.ReplaceAll(lower, "-", "0"),
		"newline":      lower + "\n",
		"shifted dash": lower[:7] + "-a-1234-4abc-89ab-cdefabcdef01",
	} {
		require.False(t, ValidResumeID(bad), name)
	}
}

func TestNormalizeResumeID(t *testing.T) {
	got, ok := NormalizeResumeID("7D4F3C8A-1234-4ABC-89AB-CDEFABCDEF01")
	require.True(t, ok)
	require.Equal(t, "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", got)

	got, ok = NormalizeResumeID("nope")
	require.False(t, ok)
	require.Empty(t, got)
}

func TestSessionSummary_ResumeIDJSON(t *testing.T) {
	b, err := json.Marshal(SessionSummary{ID: "a"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "resume_id", "omitted when empty")

	b, err = json.Marshal(SessionSummary{ID: "a", ResumeID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"})
	require.NoError(t, err)
	require.Contains(t, string(b), `"resume_id":"7d4f3c8a-1234-4abc-89ab-cdefabcdef01"`)
}

// FuzzValidResumeID checks the promise callers rely on: an accepted id is
// 36 ASCII hex-and-dash bytes, never starts with "-", and normalizes to a
// still-valid lowercase form, so it is safe as one argv element.
func FuzzValidResumeID(f *testing.F) {
	for _, s := range []string{
		"7d4f3c8a-1234-4abc-89ab-cdefabcdef01", "7D4F3C8A-1234-4ABC-89AB-CDEFABCDEF01",
		"", "-x", "--resume", "7d4f3c8a-1234-4abc-89ab-cdefabcdef01\n", "7d4f3c8a-1234-4abc-89ab-cdefabcdefé",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, ok := NormalizeResumeID(s)
		require.Equal(t, ValidResumeID(s), ok)
		if !ok {
			require.Empty(t, got)
			return
		}
		require.Len(t, s, 36)
		require.NotEqual(t, byte('-'), s[0])
		require.Equal(t, strings.ToLower(s), got)
		require.True(t, ValidResumeID(got))
		for i := 0; i < len(s); i++ {
			c := s[i]
			require.True(t, c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F', "byte %q in %q", c, s)
		}
	})
}
