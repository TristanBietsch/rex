package tui

import (
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/protocol"
)

func renderHeader(m Model) string {
	var working, needsInput, done, failed, crashed int
	for _, s := range m.Sessions {
		switch s.State {
		case protocol.StateWorking:
			working++
		case protocol.StateNeedsInput:
			needsInput++
		case protocol.StateDone:
			done++
		case protocol.StateFailed:
			failed++
		case protocol.StateCrashed:
			crashed++
		}
	}

	logo := styleHeaderApp.Render("∴ REX")
	counts := styleHeaderMeta.Render(fmt.Sprintf("  %d awaiting input · %d working · %d completed",
		needsInput, working, done))
	if failed+crashed > 0 {
		counts += styleStateFailed.Render(fmt.Sprintf("  · %d failed", failed+crashed))
	}
	newBtn := styleMuted.Render("[ + new ]")
	chips := renderFilterChips(m)

	return strings.Join([]string{logo + counts + "    " + newBtn, chips}, "\n")
}

func renderFilterChips(m Model) string {
	tools := []string{"all", "claude", "codex", "gemini", "ollama"}
	parts := make([]string, 0, len(tools)*2-1)
	for i, t := range tools {
		if i > 0 {
			parts = append(parts, styleMuted.Render(" · "))
		}
		if t == m.Filter {
			parts = append(parts, styleHeaderApp.Render(t))
		} else {
			parts = append(parts, styleDim.Render(t))
		}
	}
	return strings.Join(parts, "")
}
