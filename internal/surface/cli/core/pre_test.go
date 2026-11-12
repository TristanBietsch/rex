package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreKeepsLinesInEveryHumanFormat(t *testing.T) {
	v := View{Section{Title: "Block"}, Pre{Text: "a\n  b\n\tc"}}
	for _, f := range []string{"text", "txt", "md"} {
		out := emitKit(t, f, 80, false, Result{View: v})
		require.Contains(t, out, "a\n  b\n", f)
	}
	require.Contains(t, emitKit(t, "md", 80, false, Result{View: v}), "```\na\n")
	require.NotContains(t, emitKit(t, "agent", 80, false, Result{Cmd: "x", View: v}), "b")
	require.NotContains(t, emitKit(t, "txt", 80, false, Result{View: View{Pre{Text: "x\x1b]0;pwn\a\ny"}}}), "\x1b")
}

// A record that is still too big after the chain goes drops the flag names.
func TestShrinkRecordDropsFlagsLast(t *testing.T) {
	rec := ErrLogRecord{V: 1, Cmd: "ls", Chain: []string{"x"}}
	for range 400 {
		rec.Flags = append(rec.Flags, "-"+strings.Repeat("f", 30))
	}
	b, err := json.Marshal(rec)
	require.NoError(t, err)
	out := shrinkRecord(b)
	require.LessOrEqual(t, len(out), errLogMaxRecord)
	var back ErrLogRecord
	require.NoError(t, json.Unmarshal(out, &back))
	require.Empty(t, back.Flags)
	require.Empty(t, back.Chain)
}
