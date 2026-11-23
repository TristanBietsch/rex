package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
)

func TestAppendDelete_WritesTheSpecLineAndReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	require.NoError(t, Append(path, rec("aaaa1111", t0)))
	by := &state.Requester{Client: "rex-cli", PID: 4242, Exe: "/usr/bin/rex"}
	require.NoError(t, AppendDelete(path, "aaaa1111", by, t0.Add(time.Hour)))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, lines, 2)
	require.JSONEq(t, `{"event":"delete","session_id":"aaaa1111","deleted_by":{"client":"rex-cli","pid":4242,"exe":"/usr/bin/rex"},"ts":"2026-11-10T13:00:00Z"}`, lines[1])

	got, errs := Read([]string{path}, Filter{})
	require.Empty(t, errs)
	require.Len(t, got, 2)
	require.False(t, got[0].IsDelete())
	require.True(t, got[1].IsDelete())
	require.Equal(t, by, got[1].DeletedBy)
}

func TestAppendDelete_NoRequesterOmitsDeletedBy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	require.NoError(t, AppendDelete(path, "aaaa1111", nil, t0))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "deleted_by")
	require.Error(t, AppendDelete(path, "", nil, t0))
	require.Error(t, AppendDelete(path, "aaaa1111", nil, time.Time{}))
}

func TestRead_DeleteLinesFollowFiltersAndOldFieldsStillParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	old := rec("aaaa1111", t0)
	old.RequestedBy = &state.Requester{Client: "rex-tui", PID: 7}
	require.NoError(t, Append(path, old))
	require.NoError(t, AppendDelete(path, "aaaa1111", nil, t0.Add(time.Hour)))
	require.NoError(t, AppendDelete(path, "aaaa1111", nil, t0.Add(time.Hour))) // a synced duplicate

	got, _ := Read([]string{path}, Filter{})
	require.Len(t, got, 2, "the duplicate delete counts once")
	require.Equal(t, old.RequestedBy, got[0].RequestedBy)
	got, _ = Read([]string{path}, Filter{Since: t0.Add(30 * time.Minute)})
	require.Len(t, got, 1, "since applies to the delete's time")
	require.True(t, got[0].IsDelete())
	got, _ = Read([]string{path}, Filter{Tool: "claude"})
	require.Len(t, got, 1, "a tool filter drops delete lines")
}
