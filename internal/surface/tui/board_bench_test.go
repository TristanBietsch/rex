package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func benchSessions(n int) []protocol.SessionSummary {
	now := time.Now()
	states := []protocol.State{
		protocol.StateNeedsInput,
		protocol.StateWorking,
		protocol.StateDone,
	}
	tools := []string{"claude", "codex", "gemini", "ollama", "echo"}
	out := make([]protocol.SessionSummary, n)
	for i := 0; i < n; i++ {
		out[i] = protocol.SessionSummary{
			ID:          fmt.Sprintf("bench-%04d-4000-8000-000000000001", i),
			ShortID:     fmt.Sprintf("%04x", i%0x10000),
			ToolID:      tools[i%len(tools)],
			ModelID:     "sonnet",
			Slug:        fmt.Sprintf("task-%04d", i),
			LastLine:    "work in progress on subsystem indexing",
			State:       states[i%len(states)],
			LastEventAt: now.Add(-time.Duration(i) * time.Minute),
		}
	}
	return out
}

func benchModel(n int) Model {
	store := settings.NewStore()
	return Model{
		Sessions:    benchSessions(n),
		Filter:      "all",
		Width:       120,
		Height:      40,
		SpinnerTick: 3,
		Store:       store,
	}
}

func BenchmarkRenderBoard_10(b *testing.B) {
	m := benchModel(10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderBoard(m, 120, 30)
	}
}

func BenchmarkRenderBoard_50(b *testing.B) {
	m := benchModel(50)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderBoard(m, 120, 30)
	}
}

func BenchmarkRenderBoard_200(b *testing.B) {
	m := benchModel(200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderBoard(m, 120, 30)
	}
}
