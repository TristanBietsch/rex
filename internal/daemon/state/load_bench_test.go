package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func writeBenchSessions(b *testing.B, root string, n int) {
	b.Helper()
	sessions := filepath.Join(root, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		id := formatBenchID(i)
		dir := filepath.Join(sessions, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		sum := protocol.SessionSummary{
			ID: id, ShortID: formatBenchShort(i), ToolID: "echo", ModelID: "short",
			Slug: "bench", State: protocol.StateDone,
			StartedAt: now, LastEventAt: now, LastLine: "done",
		}
		raw, err := json.MarshalIndent(sum, "", "  ")
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644); err != nil {
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

func BenchmarkLoadAll_50(b *testing.B) {
	root := b.TempDir()
	writeBenchSessions(b, root, 50)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadAll(root); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoadAll_200(b *testing.B) {
	root := b.TempDir()
	writeBenchSessions(b, root, 200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadAll(root); err != nil {
			b.Fatal(err)
		}
	}
}
