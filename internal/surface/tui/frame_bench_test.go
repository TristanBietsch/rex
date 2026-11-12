package tui

import (
	"testing"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// BenchmarkAttachView_Frame is BenchmarkAttachView with the window frame on:
// the same popup (same layout area) plus the frame's string concatenation.
// It must stay within about 5% of the no-frame frame.
func BenchmarkAttachView_Frame(b *testing.B) {
	m := benchAttachModel(b, benchAttachCols, benchAttachRows)
	if m.Store == nil {
		m.Store = settings.NewStore()
	}
	if err := m.Store.Set("window_border", true); err != nil {
		b.Fatal(err)
	}
	m.TermW, m.TermH = m.Width+2, m.Height+2
	m = syncLayout(m)
	if !frameShown(m) {
		b.Fatal("the frame must be on")
	}
	if gc, gr := attachPopupInnerSize(m.Width, m.Height, 0); gc != benchAttachCols || gr != benchAttachRows {
		b.Fatalf("popup is %dx%d: the layout area must stay the same", gc, gr)
	}
	_, _ = m.Attach.Term.Write(claudeLikeFrame(0, benchAttachCols, benchAttachRows))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		dropAttachBackground(m, SpinnerTickMsg{})
		_ = m.View()
	}
}
