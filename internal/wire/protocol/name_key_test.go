package protocol

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSameSessionName(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"Fix Auth", "fix auth", true},
		{"fix auth", "  FIX   AUTH ", true},
		{"fix\tauth", "fix auth", true},
		{"a\u200bb", "ab", true},           // zero-width space ignored
		{"x\u00adx", "xx", true},           // soft hyphen ignored
		{"\u212aelvin", "kelvin", true},    // Kelvin sign folds to k
		{"ΣΊΣΥΦΟΣ", "σίσυφος", true},       // Greek, final sigma in the orbit
		{"Straße", "STRASSE", false},       // no full folding
		{"caf\u00e9", "cafe\u0301", false}, // no NFC
		{"alpha", "alpha2", false},
		{"", "", false},
		{"", "x", false},
		{"\u200b", "\u200b", false}, // key empty: no name
		{"red\x1b", "red", true},    // control runes dropped
	}
	for _, c := range cases {
		require.Equal(t, c.same, SameSessionName(c.a, c.b), "%q vs %q", c.a, c.b)
		require.Equal(t, c.same, SameSessionName(c.b, c.a), "%q vs %q (swapped)", c.b, c.a)
	}
}

func TestSessionNameKey(t *testing.T) {
	require.Empty(t, SessionNameKey(""))
	require.Empty(t, SessionNameKey("   "))
	require.Equal(t, SessionNameKey("ABC def"), SessionNameKey("abc DEF"))
	// A name over the limit still gets a key (legacy names on disk).
	long := strings.Repeat("Ab", MaxSessionNameRunes)
	require.Equal(t, SessionNameKey(long), SessionNameKey(strings.ToLower(long)))
	require.NotEmpty(t, SessionNameKey(long))
}
