package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestWriteSessionsTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, WriteSessionsTable(&buf, nil))
	require.Contains(t, buf.String(), "no sessions")
}

func TestWriteSessionsTable_Rows(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ShortID: "7d4f", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus", Effort: "high", Slug: "demo", LastEventAt: time.Now().Add(-2 * time.Minute)},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteSessionsTable(&buf, sessions))
	out := buf.String()
	require.Contains(t, out, "7d4f")
	require.Contains(t, out, "working")
	require.Contains(t, out, "demo")
	require.Contains(t, out, "opus · high")
}

func TestWriteAggregateLine(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{State: protocol.StateWorking},
		{State: protocol.StateWorking},
		{State: protocol.StateNeedsInput},
		{State: protocol.StateDone},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteAggregateLine(&buf, sessions))
	out := buf.String()
	require.True(t, strings.Contains(out, "1 awaiting input · 2 working · 1 completed"))
}
