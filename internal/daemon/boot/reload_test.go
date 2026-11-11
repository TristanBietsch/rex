package boot

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

type captureHandler struct{ msgs *[]string }

func (captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		*h.msgs = append(*h.msgs, a.Value.String())
		return true
	})
	return nil
}
func (h captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h captureHandler) WithGroup(string) slog.Handler      { return h }

func TestLogRegistryWarnings(t *testing.T) {
	var got []string
	old := slog.Default()
	slog.SetDefault(slog.New(captureHandler{&got}))
	t.Cleanup(func() { slog.SetDefault(old) })

	LogRegistryWarnings("/x/tools.yaml", &registry.Registry{Warnings: []string{"tool gemini is no longer built in"}})
	if !strings.Contains(strings.Join(got, "|"), "tool gemini is no longer built in") {
		t.Fatalf("warning not logged: %v", got)
	}
}
