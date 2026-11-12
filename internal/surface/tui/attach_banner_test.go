package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var bannerNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// bannerModel is an attach popup (no daemon client) on a fixed clock with the
// banner on, sized w×h.
func bannerModel(t *testing.T, w, h int, s protocol.SessionSummary) Model {
	t.Helper()
	t.Setenv("HOME", "/home/dev")
	m := boardModel(s)
	m.Store = settings.NewStore()
	m.Clock = func() time.Time { return bannerNow }
	m.Width, m.Height = w, h
	banner := attachBannerRows(m)
	cols, rows := attachPopupInnerSize(w, h, banner)
	st := &AttachState{SessionID: s.ID, Slug: s.Slug, Cols: cols, Rows: rows, BannerRows: banner,
		resize: func(uint16, uint16) error { return nil }}
	st.Term = newAttachTerm(st, cols, rows, 0)
	m.Attach = st
	m.Focus = FocusAttach
	return m
}

func bannerSession() protocol.SessionSummary {
	return protocol.SessionSummary{
		ID: "a", Slug: "billing", ToolID: "claude", State: protocol.StateWorking,
		Title: "fix billing tests", Description: "Rewriting the invoice rounding tests",
		CWD: "/home/dev/code/rex", WorkingSince: bannerNow.Add(-12 * time.Minute),
		LastEventAt: bannerNow.Add(-5 * time.Second), DescriptionAt: bannerNow.Add(-40 * time.Second),
	}
}

func TestAttachPopupInnerSize_BannerTakesOneRow(t *testing.T) {
	c0, r0 := attachPopupInnerSize(120, 40, 0)
	c1, r1 := attachPopupInnerSize(120, 40, 1)
	require.Equal(t, c0, c1)
	require.Equal(t, r0-1, r1)
	require.Equal(t, 28, r0)
	_, floor := attachPopupInnerSize(120, 10, 1)
	require.Equal(t, 8, floor)
}

func TestAttachBanner_HiddenOnShortTerminal(t *testing.T) {
	m := boardModel()
	m.Store = settings.NewStore()
	m.Height = 24
	require.Equal(t, 0, attachBannerRows(m))
	m.Height = 25
	require.Equal(t, 1, attachBannerRows(m), "the agent keeps 12 rows")
}

func TestAttachBanner_ModeSetting(t *testing.T) {
	m := boardModel()
	m.Height = 40
	require.Equal(t, 1, attachBannerRows(m), "no store: the default is on")
	m.Store = settings.NewStore()
	for mode, want := range map[string]int{"marquee": 1, "static": 1, "off": 0} {
		require.NoError(t, m.Store.Set("attach_banner", mode))
		require.Equal(t, want, attachBannerRows(m), mode)
	}
	require.Error(t, m.Store.Set("attach_banner", "wobble"))
}

func TestResizeAttach_UsesSeamAndBannerRows(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	require.Equal(t, 1, m.Attach.BannerRows)
	require.Equal(t, 27, m.Attach.Rows)
	var got [][2]uint16
	m.Attach.resize = func(c, r uint16) error { got = append(got, [2]uint16{c, r}); return nil }

	resizeAttach(m)
	require.Empty(t, got, "no change, no resize")

	m.Width, m.Height = 130, 41
	resizeAttach(m)
	require.Equal(t, [][2]uint16{{116, 28}}, got)

	// A short terminal drops the banner: one resize with the rows back.
	m.Height = 24
	resizeAttach(m)
	require.Equal(t, 0, m.Attach.BannerRows)
	require.Equal(t, [2]uint16{116, 12}, got[len(got)-1])

	// The setting going off while open also resizes (read on every resize).
	m.Height = 40
	resizeAttach(m)
	require.Equal(t, 1, m.Attach.BannerRows)
	require.NoError(t, m.Store.Set("attach_banner", "off"))
	resizeAttach(m)
	require.Equal(t, 0, m.Attach.BannerRows)
	require.Equal(t, 28, m.Attach.Rows)
}

func TestAttachInnerOrigin_WithBanner(t *testing.T) {
	for _, sz := range [][2]int{{120, 40}, {101, 33}, {200, 60}} {
		for _, mode := range []string{"static", "off"} {
			m := bannerModel(t, sz[0], sz[1], bannerSession())
			require.NoError(t, m.Store.Set("attach_banner", mode))
			resizeAttach(m)
			_, _ = m.Attach.Term.Write([]byte("Q"))
			ox, oy := attachInnerOrigin(m)
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			require.Greater(t, len(lines), oy)
			require.Equal(t, "Q", ansi.Cut(lines[oy], ox, ox+1), "size %v %s: origin (%d,%d)", sz, mode, ox, oy)
			require.Len(t, lines, sz[1], "the view fills the terminal")
		}
	}
}

func TestBannerParts(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	texts := func(items []bannerItem) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.text)
		}
		return out
	}

	sum := bannerSession()
	lead, meta := bannerParts(m, sum, false)
	require.Equal(t, "Rewriting the invoice rounding tests", lead)
	require.Equal(t, []string{"code/rex", "working 12m", "updated 40s ago"}, texts(meta))

	sum.Description, sum.DescriptionAt = "", time.Time{}
	lead, meta = bannerParts(m, sum, false)
	require.Equal(t, "task: fix billing tests", lead)
	require.Equal(t, []string{"code/rex", "working 12m"}, texts(meta))

	sum.Title, sum.State, sum.WorkingSince, sum.LastEventAt = "", protocol.StateNeedsInput, time.Time{}, bannerNow.Add(-3*time.Minute)
	sum.LastLine = "Delete the old table?"
	lead, meta = bannerParts(m, sum, false)
	require.Equal(t, "Delete the old table?", lead)
	require.Equal(t, []string{"code/rex", "waiting 3m"}, texts(meta))

	sum.LastLine = "Checking for updates"
	lead, _ = bannerParts(m, sum, false)
	require.Empty(t, lead, "raw status text is not a lead")

	sum.CWD = "/home/dev"
	_, meta = bannerParts(m, sum, false)
	require.Equal(t, "~", meta[0].text)

	sum.State, sum.EndedAt = protocol.StateDone, time.Date(2026, 1, 1, 9, 2, 0, 0, time.UTC)
	_, meta = bannerParts(m, sum, false)
	require.Equal(t, "ended "+sum.EndedAt.Local().Format("15:04"), meta[1].text)

	sum.EndedAt = time.Time{}
	_, meta = bannerParts(m, sum, true)
	require.Equal(t, "ended "+sum.LastEventAt.Local().Format("15:04"), meta[1].text, "falls back to the last event")

	// A stale description is muted.
	sum = bannerSession()
	sum.DescriptionAt = bannerNow.Add(-3 * time.Minute)
	_, meta = bannerParts(m, sum, false)
	require.Equal(t, "updated 3m ago", meta[2].text)
	require.True(t, meta[2].muted)
}

func TestShortCWD(t *testing.T) {
	t.Setenv("HOME", "/home/dev")
	for in, want := range map[string]string{
		"/home/dev":          "~",
		"/home/dev/rex":      "~/rex",
		"/home/dev/a/b/rex":  "b/rex",
		"/srv/app":           "/srv/app",
		"/srv/x/y/app":       "y/app",
		"":                   "",
		"/home/dev/a\x1b[2J": "~/a",
	} {
		require.Equal(t, want, shortCWD(in), in)
	}
}

func TestBanner_SanitizesUntrusted(t *testing.T) {
	evil := "\x1b]0;evil\x07\x1b[2Jpwn\u202eed\nsecond line\r\x00"
	s := bannerSession()
	s.Description, s.Title, s.CWD, s.LastLine = evil, evil, "/home/dev/"+evil, evil+"?"
	for _, mode := range []string{"static", "marquee"} {
		m := bannerModel(t, 120, 40, s)
		require.NoError(t, m.Store.Set("attach_banner", mode))
		out := renderAttachBanner(m, m.Attach, m.Attach.Cols)
		for _, bad := range []string{"\x07", "\x00", "\n", "\r", "\u202e", "evil\x07"} {
			require.NotContains(t, out, bad, mode)
		}
		require.NotContains(t, out, "\x1b]", mode)
		require.NotContains(t, out, "\x1b[2J", mode)
		require.Equal(t, m.Attach.Cols, ansi.StringWidth(out), mode)
		require.Contains(t, ansi.Strip(out), "pwn", mode)
	}
}

func TestBanner_RendersAboveRuleAndNeverForwardsMouse(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	_, oy := attachInnerOrigin(m)
	require.Contains(t, lines[oy-2], "↳ Rewriting the invoice", "banner sits right above the rule")
	require.Contains(t, lines[oy-1], "───")

	_, _ = m.Attach.Term.Write([]byte("\x1b[?1000h\x1b[?1006h")) // the agent tracks the mouse
	ox, _ := attachInnerOrigin(m)
	for _, y := range []int{oy - 2, oy - 3} { // banner, title bar
		wheel := tea.MouseMsg{X: ox + 3, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}
		out, cmd := updateAttachMouse(m, wheel)
		require.Nil(t, cmd)
		require.Zero(t, out.Attach.wheelNotches, "row %d is chrome", y)
	}
	wheel := tea.MouseMsg{X: ox + 3, Y: oy, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}
	out, _ := updateAttachMouse(m, wheel)
	require.Equal(t, 1, out.Attach.wheelNotches)
	require.Contains(t, string(out.Attach.wheelBuf), ";4;1M", "the first agent row is cy 0")
}

func TestBanner_StaticSnapshots(t *testing.T) {
	cases := map[string]func(*Model){
		"static": func(m *Model) { require.NoError(t, m.Store.Set("attach_banner", "static")) },
		"off":    func(m *Model) { require.NoError(t, m.Store.Set("attach_banner", "off")) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m := bannerModel(t, 120, 40, bannerSession())
			setup(&m)
			resizeAttach(m)
			_, _ = m.Attach.Term.Write([]byte("agent output\r\n"))
			out := ansi.Strip(m.View())
			lines := strings.Split(out, "\n")
			require.Len(t, lines, 40)
			for _, l := range lines {
				require.LessOrEqual(t, ansi.StringWidth(l), 120)
			}
			golden.Check(t, "testdata/attach_banner_"+name+".txtar", golden.Section{Name: "view", Body: out + "\n"})
		})
	}
}

func TestBanner_NarrowDropsMeta(t *testing.T) {
	m := bannerModel(t, 80, 40, bannerSession()) // cols 66
	require.NoError(t, m.Store.Set("attach_banner", "static"))
	out := ansi.Strip(renderAttachBanner(m, m.Attach, m.Attach.Cols))
	require.Contains(t, out, "working 12m")
	m = bannerModel(t, 70, 40, bannerSession()) // cols 56 < 60
	require.NoError(t, m.Store.Set("attach_banner", "static"))
	out = ansi.Strip(renderAttachBanner(m, m.Attach, m.Attach.Cols))
	require.NotContains(t, out, "working 12m")
	require.Contains(t, out, "Rewriting")
}

func TestBanner_DescAnimShowsFrame(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	require.NoError(t, m.Store.Set("attach_banner", "static"))
	m.DescAnim = map[string]DescAnim{"a": {From: "", To: "Rewriting the invoice rounding tests", Effect: "typewriter",
		StartedAt: bannerNow, Duration: time.Second}}
	out := ansi.Strip(renderAttachBanner(m, m.Attach, m.Attach.Cols))
	require.NotContains(t, out, "Rewriting the invoice rounding tests", "frame 0 of a typewriter")
	m.Clock = func() time.Time { return bannerNow.Add(2 * time.Second) }
	out = ansi.Strip(renderAttachBanner(m, m.Attach, m.Attach.Cols))
	require.Contains(t, out, "Rewriting the invoice rounding tests")
}

func TestAttachBannerSetting_Registered(t *testing.T) {
	st := settings.NewStore()
	require.Equal(t, "marquee", st.Get("attach_banner"))
	require.Error(t, st.Set("attach_banner", "blink"))
}

func TestAttachCommand_BannerToggleResizes(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	var got [][2]uint16
	m.Attach.resize = func(c, r uint16) error { got = append(got, [2]uint16{c, r}); return nil }
	require.Equal(t, 27, m.Attach.Rows)

	m, _ = executeAttachCommand(m, "banner")
	require.True(t, m.bannerHidden)
	require.Equal(t, 0, m.Attach.BannerRows)
	require.Equal(t, 28, m.Attach.Rows)
	require.Equal(t, [][2]uint16{{106, 28}}, got, "the agent gets one more row")
	_, vtRows := m.Attach.Term.Size()
	require.Equal(t, 28, vtRows, "vt10x follows")
	require.Equal(t, FocusAttach, m.Focus)
	require.Contains(t, m.Notice, "::banner shows it")
	require.NotContains(t, ansi.Strip(m.View()), "↳ Rewriting")
	require.Equal(t, "marquee", m.Store.Get("attach_banner"), "this run only: the setting is untouched")
	require.Equal(t, 0, attachBannerRows(m), "a popup opened now stays without it")

	m, _ = executeAttachCommand(m, "banner")
	require.False(t, m.bannerHidden)
	require.Equal(t, 1, m.Attach.BannerRows)
	require.Equal(t, [2]uint16{106, 27}, got[len(got)-1], "and one fewer when it returns")
	require.Equal(t, bannerNow, m.Attach.BannerSince, "a returning marquee starts from its beginning")
	require.Contains(t, ansi.Strip(m.View()), "↳ Rewriting")
}

func TestAttachCommand_BannerToggleKeepsScrollInRange(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	m.Attach.sb = newAttachScrollback("a", 100)
	m.Attach.Term = newAttachTerm(m.Attach, m.Attach.Cols, m.Attach.Rows, 100)
	for i := range 60 {
		_, _ = m.Attach.Term.Write([]byte(strings.Repeat("x", 3) + string(rune('a'+i%26)) + "\r\n"))
	}
	m.Attach.ScrollOff = m.Attach.sb.Len()
	m, _ = executeAttachCommand(m, "banner") // grows
	m, _ = executeAttachCommand(m, "banner") // shrinks: rows go to scrollback
	require.LessOrEqual(t, m.Attach.ScrollOff, m.Attach.sb.Len())
}

func TestAttachCommand_BannerOffInSettingsSaysSo(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	require.NoError(t, m.Store.Set("attach_banner", "off"))
	resizeAttach(m)
	m, _ = executeAttachCommand(m, "banner")
	require.False(t, m.bannerHidden, "nothing to toggle")
	require.Contains(t, m.Notice, "attach_banner")
	require.Equal(t, 0, m.Attach.BannerRows)
}

func TestAttachCommand_BannerTooShortSaysSo(t *testing.T) {
	m := bannerModel(t, 120, 24, bannerSession())
	require.Equal(t, 0, m.Attach.BannerRows)
	m, _ = executeAttachCommand(m, "banner") // hides (already not shown)
	m, _ = executeAttachCommand(m, "banner") // shows: still no room
	require.Equal(t, 0, m.Attach.BannerRows)
	require.Contains(t, m.Notice, "too short")
}

func TestAttachCommandBar_HintListsBanner(t *testing.T) {
	m := bannerModel(t, 160, 40, bannerSession())
	require.Contains(t, ansi.Strip(renderAttachCommandBar(m, 200)), "banner")
}

// elsewhereBoard is bannerModel with other sessions in every bucket and the
// overview on.
func elsewhereBoard(t *testing.T, on bool) Model {
	t.Helper()
	m := bannerModel(t, 160, 40, bannerSession())
	m.Sessions = append(m.Sessions,
		protocol.SessionSummary{ID: "r1", State: protocol.StateNeedsInput, WaitReason: protocol.WaitReview},
		protocol.SessionSummary{ID: "r2", State: protocol.StateDone},
		protocol.SessionSummary{ID: "n1", State: protocol.StateNeedsInput, LastLine: "Deploy?"},
		protocol.SessionSummary{ID: "f1", State: protocol.StateFailed},
		protocol.SessionSummary{ID: "w1", State: protocol.StateWorking},
	)
	require.NoError(t, m.Store.Set("overview_enabled", on))
	if on {
		m.Overview = &protocol.Overview{Enabled: true, GeneratedAt: bannerNow, Headline: "billing is done; \x1b[2Jdeploy asks a question"}
	}
	return m
}

func TestBanner_ElsewhereExcludesSelf(t *testing.T) {
	m := elsewhereBoard(t, true)
	// The attached session is itself needs-you; it is not counted.
	m.Sessions[0].State = protocol.StateNeedsInput
	_, meta := bannerParts(m, m.Sessions[0], false)
	var texts []string
	for _, it := range meta {
		texts = append(texts, it.text)
	}
	require.Contains(t, texts, "elsewhere: 2 to review · 1 ready · 1 failed")
	require.Contains(t, texts, "» billing is done; deploy asks a question", "the S4 headline follows, flattened")
}

func TestBanner_ElsewhereNeedsOverviewOn(t *testing.T) {
	m := elsewhereBoard(t, false)
	m.Overview = &protocol.Overview{Enabled: true, Headline: "stale"}
	_, meta := bannerParts(m, m.Sessions[0], false)
	for _, it := range meta {
		require.NotContains(t, it.text, "elsewhere")
		require.NotContains(t, it.text, "stale")
	}
}

func TestBanner_ElsewhereVariants(t *testing.T) {
	m := elsewhereBoard(t, true)
	m.Overview = nil
	m.Sessions = m.Sessions[:1]
	_, meta := bannerParts(m, m.Sessions[0], false)
	for _, it := range meta {
		require.NotContains(t, it.text, "elsewhere", "no other sessions, nothing to say")
	}
	m.Sessions = append(m.Sessions, protocol.SessionSummary{ID: "w1", State: protocol.StateWorking})
	_, meta = bannerParts(m, m.Sessions[0], false)
	require.Equal(t, "elsewhere: quiet", meta[len(meta)-1].text)
}

func TestBanner_ElsewhereShowsInMarquee(t *testing.T) {
	m := elsewhereBoard(t, true)
	sum := m.Sessions[0]
	lead, meta := bannerParts(m, sum, false)
	require.Contains(t, marqueeText(sum, lead, meta), "elsewhere: 2 to review")
}
