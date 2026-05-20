package protocol

import (
	"bytes"
	"testing"
	"time"
)

func benchSessionUpdated() SessionUpdated {
	return SessionUpdated{
		SessionID: "bench-0001-4000-8000-000000000001",
		Patch: map[string]any{
			"state":        string(StateWorking),
			"last_line":    "events_org_ts index live — p95 38ms",
			"tokens":       int64(12500),
			"output_bytes": int64(50000),
		},
	}
}

func BenchmarkCodec_SessionUpdatedRoundTrip(b *testing.B) {
	upd := benchSessionUpdated()
	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		w := NewWriter(&buf)
		if err := w.WriteEvent(EventSessionUpdated, "", upd); err != nil {
			b.Fatal(err)
		}
		r := NewReader(&buf)
		env, err := r.Read()
		if err != nil {
			b.Fatal(err)
		}
		if env.Type != EventSessionUpdated {
			b.Fatalf("type %q", env.Type)
		}
	}
}

func BenchmarkCodec_SnapshotRoundTrip(b *testing.B) {
	now := time.Now().UTC()
	sessions := make([]SessionSummary, 50)
	for i := range sessions {
		sessions[i] = SessionSummary{
			ID: formatBenchID(i), ShortID: formatBenchShort(i),
			ToolID: "claude", ModelID: "sonnet", Slug: "perf-audit",
			State: StateWorking, StartedAt: now, LastEventAt: now,
			LastLine: "indexing events_org_ts",
		}
	}
	snap := Snapshot{Sessions: sessions, Filter: "all"}
	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		w := NewWriter(&buf)
		if err := w.WriteEvent(EventSnapshot, "", snap); err != nil {
			b.Fatal(err)
		}
		r := NewReader(&buf)
		if _, err := r.Read(); err != nil {
			b.Fatal(err)
		}
	}
}

func formatBenchID(i int) string {
	return "bench-" + formatBenchShort(i) + "-4000-8000-000000000001"
}

func formatBenchShort(i int) string {
	const hex = "0123456789abcdef"
	s := make([]byte, 4)
	for j := 3; j >= 0; j-- {
		s[j] = hex[i&0xf]
		i >>= 4
	}
	return string(s)
}
