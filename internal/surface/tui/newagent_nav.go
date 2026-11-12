package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
)

// Compose navigation on the new-agent page, vim-style. The page has two
// modes:
//
//   - Editing (insert mode): keys type into the task editor. enter launches
//     (R16 D2), ctrl+j and alt+enter add a line, esc leaves the editor
//     (closing an open @ list first), tab / shift+tab leave it for the next
//     or previous step.
//   - Navigation: the form is one list (agent, task, each context item,
//     trust, launch) that j/k walk with the shared navigator (nav.go); h
//     focuses the step rail and l comes back; i, a, A or enter on the task
//     edit it again; esc or q closes the page (the draft is kept).
//
// ctrl+s launches, ctrl+g opens the Agent screen and ctrl+o the import
// picker in both modes.

// naRows is the number of rows j/k walk on Compose: agent, task, the
// context items (one row when there are none), trust and launch.
func naRows(st *NewAgentState) int { return 4 + max(len(st.Items), 1) }

// naRow is the cursor's row in naRows.
func naRow(st *NewAgentState) int {
	switch st.Section {
	case naAgent:
		return 0
	case naTask:
		return 1
	case naContext:
		return 2 + min(max(st.ItemIdx, 0), max(len(st.Items)-1, 0))
	case naTrust:
		return naRows(st) - 2
	default:
		return naRows(st) - 1
	}
}

// naSetRow puts the cursor on row r of naRows.
func naSetRow(st *NewAgentState, r int) {
	n := naRows(st)
	switch r = min(max(r, 0), n-1); r {
	case 0:
		naFocus(st, naAgent)
	case 1:
		naFocus(st, naTask)
	case n - 2:
		naFocus(st, naTrust)
	case n - 1:
		naFocus(st, naLaunchRow)
	default:
		naFocus(st, naContext)
		st.ItemIdx = r - 2
	}
}

// naFocus moves the cursor to section s, leaving the editor; the context
// list keeps its item.
func naFocus(st *NewAgentState, s naSection) {
	st.Section, st.Editing, st.Complete = s, false, nil
}

// naEdit enters the task editor (insert mode).
func naEdit(st *NewAgentState) {
	st.Section, st.Editing, st.OnRail = naTask, true, false
}

// naStepSection is the section dir steps (±1) from the current one; wrap
// cycles past the ends (tab), else it clamps ([ ], the rail's j/k).
func naStepSection(st *NewAgentState, dir int, wrap bool) naSection {
	next := int(st.Section) + dir
	if wrap {
		return naSection((next + int(naSectionCount)) % int(naSectionCount))
	}
	return naSection(min(max(next, 0), int(naSectionCount)-1))
}

// naInsertKey is a key while typing in the task editor.
func naInsertKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	if st.Complete != nil {
		if m, cmd, ok := naCompleteKey(m, k); ok {
			return m, cmd
		}
	}
	switch k.Type {
	case tea.KeyEsc:
		st.Editing, st.Complete = false, nil
		return m, nil
	case tea.KeyTab, tea.KeyShiftTab:
		dir := 1
		if k.Type == tea.KeyShiftTab {
			dir = -1
		}
		naFocus(st, naStepSection(st, dir, true))
		playNav(m)
		return m, nil
	case tea.KeyCtrlO:
		return openImport(m), nil
	case tea.KeyCtrlS:
		return launchNewAgent(m)
	case tea.KeyCtrlG:
		return openAgentScreen(m), nil
	}
	m, cmd := newAgentEditKey(m, k)
	if m.NewAgent != nil {
		naSyncComplete(m.NewAgent, k)
	}
	return m, cmd
}

// naNormalKey is a key on Compose outside the editor.
func naNormalKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	key := k.String()
	narrow := pageNarrow(m.Width)
	if narrow {
		st.OnRail = false // no rail to focus
	}
	switch key {
	case "esc", "q":
		return closeNewAgent(m, true), nil
	case "ctrl+o":
		return openImport(m), nil
	case "ctrl+s":
		return launchNewAgent(m)
	case "ctrl+g":
		return openAgentScreen(m), nil
	case "?":
		return openNewAgentHelp(m), nil
	case "tab", "shift+tab", "[", "]":
		dir := 1
		if key == "shift+tab" || key == "[" {
			dir = -1
		}
		naFocus(st, naStepSection(st, dir, key == "tab" || key == "shift+tab"))
		playNav(m)
		return m, nil
	case "1", "2", "3", "4", "5":
		naFocus(st, naSection(key[0]-'1'))
		playNav(m)
		return m, nil
	case "i", "a", "A", "I":
		naEdit(st)
		switch key {
		case "a":
			st.Task.Move(0, 1)
		case "A":
			st.Task.End()
		case "I":
			st.Task.Home()
		}
		return m, nil
	case "h", "left":
		if narrow {
			naFocus(st, naStepSection(st, -1, false))
		} else {
			st.OnRail = true
		}
		playNav(m)
		return m, nil
	case "l", "right":
		if narrow {
			naFocus(st, naStepSection(st, 1, false))
		} else {
			st.OnRail = false
		}
		playNav(m)
		return m, nil
	}
	if mv := vimMotion(key, &st.gPending); mv != navNone {
		if mv != navPending {
			naMove(m, mv)
		}
		return m, nil
	}
	if st.OnRail {
		if key == "enter" {
			st.OnRail = false
			playNav(m)
		}
		return m, nil
	}
	return naSectionKey(m, k)
}

// naMove moves the cursor by mv: through the steps on the rail, else
// through the form's rows.
func naMove(m Model, mv navMove) {
	st := m.NewAgent
	before := naRow(st)
	if st.OnRail {
		s := naSection(navTarget(mv, int(st.Section), int(naSectionCount), int(naSectionCount)))
		if s != st.Section {
			naFocus(st, s)
		}
	} else {
		naSetRow(st, navTarget(mv, before, naRows(st), st.itemsView.H+4))
	}
	if naRow(st) != before {
		playNav(m)
	}
}

// naSectionKey is a key on the focused section's content: enter opens or
// acts on it; the context list also takes x/d/delete, r and e.
func naSectionKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	key := k.String()
	switch st.Section {
	case naAgent:
		if key == "enter" {
			return openAgentScreen(m), nil
		}
	case naTask:
		switch key {
		case "enter":
			naEdit(st)
		case "e", "ctrl+e":
			return m, editInEditor(-1, st.Task.String())
		}
	case naContext:
		if m, cmd, ok := naContextKey(m, k); ok {
			return m, cmd
		}
	case naTrust:
		if key == "enter" || key == " " {
			return naCycleTrust(m), nil
		}
	case naLaunchRow:
		if key == "enter" {
			return launchNewAgent(m)
		}
	case naSectionCount:
	}
	return m, nil
}

// naCycleTrust is enter or space on the trust step: ask, then this session,
// then always (skipped where the folder cannot be saved), then ask again,
// through the same checks as ctrl+t and ctrl+a.
func naCycleTrust(m Model) Model {
	st := m.NewAgent
	want := tea.KeyMsg{Type: tea.KeyCtrlT}
	if st.Trust != trustAsk {
		want = tea.KeyMsg{Type: tea.KeyCtrlA} // session → always; always → ask
	}
	next, mode, _ := trustKeyToggle(m, want, st.Trust)
	if st.Trust == trustSession && next.Err != "" {
		next.Err, mode = "", trustAsk // always cannot be saved here: back to ask
	}
	if next.Err != "" {
		next.NewAgent.Err = "✗ " + next.Err
		next.Err = ""
	}
	next.NewAgent.Trust = mode
	return next
}

// naContextKey is a key on the Compose context list; ok false when it is
// none of the list's keys.
func naContextKey(m Model, k tea.KeyMsg) (Model, tea.Cmd, bool) {
	st := m.NewAgent
	if len(st.Items) == 0 {
		return m, nil, false
	}
	st.ItemIdx = min(max(st.ItemIdx, 0), len(st.Items)-1)
	switch {
	case k.Type == tea.KeyEnter:
		st.Screen, st.PrevScroll = naPreview, 0
		naLoadPreview(st)
		playNav(m)
	case k.Type == tea.KeyDelete || isRune(k, 'x') || isRune(k, 'd'):
		removeItem(st, st.ItemIdx)
		if len(st.Items) == 0 {
			naFocus(st, naContext)
		}
	case isRune(k, 'r'):
		naStartRename(st)
	case k.Type == tea.KeyCtrlE || isRune(k, 'e'):
		if it := st.Items[st.ItemIdx]; it.Kind == spawnctx.Paste {
			return m, editInEditor(st.ItemIdx, it.Text), true
		}
		st.Err = "! only a paste can be edited here; a file is edited in its own editor"
	default:
		return m, nil, false
	}
	return m, nil, true
}

// updateNewAgentMouse is the wheel on the new-agent page: one row, as j/k,
// on the pane under the pointer (the step rail or the form; on the Agent
// screen the agents or the right column; the preview scrolls).
func updateNewAgentMouse(m Model, msg tea.MouseMsg) Model {
	st := m.NewAgent
	mv := wheelMotion(msg)
	if st == nil || mv == navNone || st.Launching || st.Renaming || len(st.Pending) > 0 {
		return m
	}
	m.Err, st.Err = "", ""
	st.Rev++
	st.gPending = false
	narrow := pageNarrow(m.Width)
	left := !narrow && msg.X-frameInset(m) < naRailW+1
	switch st.Screen {
	case naCompose:
		st.Editing, st.OnRail = false, left
		naMove(m, mv)
	case naAgentScreen:
		if st.Filtering {
			return m
		}
		switch {
		case narrow: // one pane shows: the wheel moves it
		case left:
			st.Pane = naPaneAgents
		case st.Pane == naPaneAgents:
			st.Pane = naPaneModels
		}
		naAgentMove(m, mv)
	case naPreview:
		naPreviewScroll(m, mv)
	case naImportScreen:
	}
	return m
}
