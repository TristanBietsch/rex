package pty

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const agyConv = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d" // synthetic

// Antigravity prints its conversation id on exit. The builtin capture_regex
// reads it from the normalized screen text, SGR codes and all, and the last
// match wins. Synthetic transcript, not a real capture.
func TestSupervisor_CapturesAgyResumeIDFromExitText(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	agy, ok := reg.Find("antigravity")
	require.True(t, ok)
	re, err := agy.Resume.CaptureRegexp()
	require.NoError(t, err)
	require.NotNil(t, re)

	stateDir := t.TempDir()
	store, sess := newResumeSession(t, "agy1")
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command: []string{"bash", "-c", `printf 'work done\r\n\033[1mResume with -c (or command below):\033[0m\r\n` +
			`\033[2magy --conversation=` + agyConv + `\033[0m\r\n'`},
		ResumeCapture: re,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))

	got, _ := store.Get("agy1")
	require.Equal(t, protocol.StateDone, got.State)
	require.Equal(t, agyConv, got.Summary().ResumeID)
}

// An id that is not a UUID in agy's exit line is not captured.
func TestAgyCaptureRegexRejectsNonUUID(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	agy, _ := reg.Find("antigravity")
	re, err := agy.Resume.CaptureRegexp()
	require.NoError(t, err)
	require.Nil(t, re.FindStringSubmatch("agy --conversation=--evil"))
	require.Nil(t, re.FindStringSubmatch("agy --conversation=short"))
	m := re.FindStringSubmatch("agy --conversation=" + agyConv)
	require.Equal(t, agyConv, m[1])
}
