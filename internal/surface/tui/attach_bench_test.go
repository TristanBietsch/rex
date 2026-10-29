package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"
	"github.com/muesli/termenv"
)

// benchAttachModel is a 200x68 screen with the attach popup open on a full
// 186x52 emulator. colorEvery > 0 changes the 256-color foreground every
// that many cells (1 = every cell, the worst case); 0 writes plain text.
func benchAttachModel(colorEvery int) Model {
	cols, rows := attachPopupInnerSize(200, 68)
	term := vt10x.New(vt10x.WithSize(cols, rows))
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if colorEvery > 0 && x%colorEvery == 0 {
				fmt.Fprintf(&sb, "\x1b[38;5;%dm", (x/colorEvery+y)%200+16)
			}
			sb.WriteByte(byte('a' + x%26))
		}
		if y < rows-1 {
			sb.WriteString("\r\n")
		}
	}
	_, _ = term.Write([]byte(sb.String()))
	return Model{Width: 200, Height: 68, Focus: FocusAttach, Filter: "all",
		Attach: &AttachState{SessionID: "1", Slug: "bench", Term: term, Cols: cols, Rows: rows}}
}

func benchRenderAttach(b *testing.B, colorEvery int) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	m := benchAttachModel(colorEvery)
	b.ReportAllocs()
	for b.Loop() {
		_ = renderAttach(m)
	}
}

func BenchmarkRenderAttach_ColorPerCell(b *testing.B) { benchRenderAttach(b, 1) }
func BenchmarkRenderAttach_ColorRuns8(b *testing.B)   { benchRenderAttach(b, 8) }
func BenchmarkRenderAttach_Plain(b *testing.B)        { benchRenderAttach(b, 0) }
