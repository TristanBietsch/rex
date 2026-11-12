package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerb(t *testing.T) {
	for args, want := range map[string]string{
		"ls":                "ls",
		"-o json ls -n 3":   "ls",
		"--agent complete":  "complete",
		"-json config list": "config",
		"":                  "",
		"-o":                "",
	} {
		var in []string
		if args != "" {
			in = strings.Fields(args)
		}
		require.Equal(t, want, Verb(in), args)
	}
}
