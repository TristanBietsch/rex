package setup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckSummarizerEndpoint(t *testing.T) {
	env := func(v string) func(string) string {
		return func(string) string { return v }
	}
	r := checkSummarizerEndpoint(env(""), false)
	require.Equal(t, checkPass, r.Status)
	require.Contains(t, r.Detail, "local http://127.0.0.1:11434")

	r = checkSummarizerEndpoint(env("0.0.0.0"), false)
	require.Equal(t, checkPass, r.Status)
	require.Contains(t, r.Detail, "http://127.0.0.1:11434")

	r = checkSummarizerEndpoint(env("https://gpu.example"), false)
	require.Equal(t, checkFail, r.Status)
	require.Contains(t, r.Detail, "summary_allow_remote")
	require.Contains(t, r.Detail, "OLLAMA_HOST")

	r = checkSummarizerEndpoint(env("https://gpu.example"), true)
	require.Equal(t, checkWarn, r.Status)
	require.Contains(t, r.Detail, "remote")

	r = checkSummarizerEndpoint(env("http://gpu.example"), true)
	require.Equal(t, checkFail, r.Status)
	require.Contains(t, r.Detail, "https")

	r = checkSummarizerEndpoint(env("http://user:secret@127.0.0.1"), false)
	require.Equal(t, checkFail, r.Status)
	require.NotContains(t, r.Detail, "secret")
}
