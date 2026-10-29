package protocol

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSlug(t *testing.T) {
	for _, ok := range []string{"alpha", "cc.opus.fix-auth-bug", "fix-bug-2", "ünïcode"} {
		require.NoError(t, ValidateSlug(ok), ok)
	}
	cases := map[string]string{
		"":                                  "required",
		" ":                                 "whitespace",
		"a b":                               "whitespace",
		"a\tb":                              "whitespace",
		"a\nb":                              "whitespace",
		"a\u00a0b":                          "whitespace",
		"a\x1b[31m":                         "control",
		"a\u202eb":                          "control",
		"a\xffb":                            "UTF-8",
		strings.Repeat("s", MaxSlugRunes+1): "exceeds",
	}
	require.NoError(t, ValidateSlug(strings.Repeat("é", MaxSlugRunes)), "cap counts runes")
	for in, want := range cases {
		require.ErrorContains(t, ValidateSlug(in), want, "%q", in)
	}
}
