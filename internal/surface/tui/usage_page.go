package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// usageTimeout bounds one UsageQuery: the daemon may poll a tool first.
const usageTimeout = 20 * time.Second

// usageStaleAfter is the age past which a quota reading is dimmed as stale
// (the daemon's own rule, see protocol.UsageQuota.At).
const usageStaleAfter = 15 * time.Minute

// usageRanges is the range order, left to right.
var usageRanges = []string{protocol.UsageRangeToday, protocol.UsageRange7d, protocol.UsageRange30d}

// usageQuerier is the daemon call behind the page. *client.Client satisfies
// it; tests swap a fake.
type usageQuerier interface {
	UsageQuery(ctx context.Context, q protocol.UsageQuery) (protocol.UsageReport, error)
}

// UsageState is the :usage page: the range, the daemon's report for it, and
// the filter and scroll.
type UsageState struct {
	Range     string
	Report    *protocol.UsageReport
	Err       string // why there is no report: usage off, old daemon, request failed
	Loading   bool
	Filter    string
	Filtering bool
	Scroll    int
	FetchedAt time.Time
	seq       int // the request this page waits for; older replies are dropped
}

// usageReportMsg is the daemon's answer to one UsageQuery.
type usageReportMsg struct {
	seq    int
	rng    string
	report protocol.UsageReport
	err    error
}

func usageQuerierOf(m Model) usageQuerier {
	if m.usager != nil {
		return m.usager
	}
	if m.Client == nil {
		return nil
	}
	return m.Client
}

// runUsage is the `:usage` command: it opens the page, or closes it when it
// is open. The usage flag is checked by executeCommand.
func runUsage(m Model, _ cmdCall) (Model, tea.Cmd) {
	if m.Focus == FocusUsage {
		return closeUsage(m), nil
	}
	return openUsage(m)
}

func openUsage(m Model) (Model, tea.Cmd) {
	m.Focus = FocusUsage
	m.Err = ""
	m.UsagePage = &UsageState{Range: protocol.UsageRangeToday}
	return fetchUsage(m)
}

func closeUsage(m Model) Model {
	m.Focus = FocusBoard
	m.UsagePage = nil
	return m
}

// fetchUsage asks the daemon for the page's range, dropping any reply still
// in flight for an older request.
func fetchUsage(m Model) (Model, tea.Cmd) {
	st := *m.UsagePage
	q := usageQuerierOf(m)
	if q == nil {
		st.Err = "not connected to the daemon"
		st.Loading = false
		m.UsagePage = &st
		return m, nil
	}
	m.usageSeq++
	st.seq = m.usageSeq
	st.Loading = true
	st.Err = ""
	m.UsagePage = &st
	rng, seq := st.Range, st.seq
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
		defer cancel()
		rep, err := q.UsageQuery(ctx, protocol.UsageQuery{Range: rng})
		return usageReportMsg{seq: seq, rng: rng, report: rep, err: err}
	}
}

// applyUsageReport stores a reply, unless the page closed or moved on.
func applyUsageReport(m Model, msg usageReportMsg) Model {
	if m.UsagePage == nil || msg.seq != m.UsagePage.seq {
		return m
	}
	st := *m.UsagePage
	st.Loading = false
	if msg.err != nil {
		st.Err = usageErrText(msg.err)
		m.UsagePage = &st
		return m
	}
	rep := msg.report
	st.Report = &rep
	st.Err = ""
	st.FetchedAt = m.now()
	m.UsagePage = &st
	return m
}

// usageErrText says why a UsageQuery failed, in the page's words.
func usageErrText(err error) string {
	var ce *client.Error
	if errors.As(err, &ce) && ce.Code == protocol.ErrCodeBadIntent {
		switch {
		case ce.Message == "intent not implemented":
			return "update daemon: run 'rex daemon restart'"
		case strings.Contains(ce.Message, "usage is off"):
			return offMessage("usage")
		}
	}
	return protocol.FlattenDisplayText(err.Error())
}

// updateUsageKey handles keys while the page is open.
func updateUsageKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := *m.UsagePage
	if st.Filtering {
		switch k.Type {
		case tea.KeyEsc:
			st.Filter, st.Filtering, st.Scroll = "", false, 0
		case tea.KeyEnter:
			st.Filtering = false
		case tea.KeyBackspace:
			if r := []rune(st.Filter); len(r) > 0 {
				st.Filter, st.Scroll = string(r[:len(r)-1]), 0
			}
		case tea.KeyCtrlU:
			st.Filter, st.Scroll = "", 0
		case tea.KeyRunes, tea.KeySpace:
			add := string(k.Runes)
			if k.Type == tea.KeySpace {
				add = " "
			}
			add = strings.Map(func(r rune) rune {
				if r < ' ' || r == 0x7f {
					return -1
				}
				return r
			}, add)
			if len([]rune(st.Filter+add)) <= helpMaxFilter {
				st.Filter, st.Scroll = st.Filter+add, 0
			}
		default:
		}
		m.UsagePage = &st
		return m, nil
	}

	switch k.String() {
	case "esc":
		if st.Filter != "" {
			st.Filter, st.Scroll = "", 0
			m.UsagePage = &st
			return m, nil
		}
		return closeUsage(m), nil
	case "q", ":":
		return closeUsage(m), nil
	case "/":
		st.Filtering = true
	case "left", "h", "right", "l", "1", "2", "3":
		next := usageRangeKey(st.Range, k.String())
		if next == st.Range {
			return m, nil
		}
		st.Range, st.Scroll = next, 0
		m.UsagePage = &st
		return fetchUsage(m)
	case "r":
		return fetchUsage(m)
	case "j", "down":
		st.Scroll++
	case "k", "up":
		st.Scroll--
	case "g", "home":
		st.Scroll = 0
	case "G", "end":
		st.Scroll = 1 << 30
	default:
	}
	m.UsagePage = &st
	m.UsagePage.Scroll = clampUsageScroll(m)
	return m, nil
}

// usageRangeKey is the range a key selects: 1/2/3 pick one, the arrows step
// (and stop at the ends).
func usageRangeKey(cur, k string) string {
	i := 0
	for j, r := range usageRanges {
		if r == cur {
			i = j
		}
	}
	switch k {
	case "1", "2", "3":
		return usageRanges[int(k[0]-'1')]
	case "left", "h":
		i = max(i-1, 0)
	case "right", "l":
		i = min(i+1, len(usageRanges)-1)
	}
	return usageRanges[i]
}

func clampUsageScroll(m Model) int {
	w, h := helpSize(m)
	lay := layoutUsage(m, w, h)
	return min(max(m.UsagePage.Scroll, 0), lay.maxScroll)
}

// ---- model of the table ----------------------------------------------------

// usageLine is one body line of the table, before layout.
type usageLine struct {
	kind  usageKind
	name  string
	row   protocol.UsageRow
	quota *protocol.UsageQuota // the quota drawn on this line, if any
	empty bool                 // a tool with a quota but no usage in the range
}

type usageKind int

const (
	usageTool usageKind = iota
	usageModel
	usageExtraQuota // a further window of the tool above
)

// usageQuotasFor lists the quotas of one tool and model, in report order.
func usageQuotasFor(qs []protocol.UsageQuota, tool, model string) []protocol.UsageQuota {
	var out []protocol.UsageQuota
	for _, q := range qs {
		if q.Tool == tool && q.Model == model {
			out = append(out, q)
		}
	}
	return out
}

// usageMatches reports whether name contains the filter, ignoring case.
func usageMatches(name, filter string) bool {
	return filter == "" || strings.Contains(strings.ToLower(name), strings.ToLower(filter))
}

// usageLines flattens the report into table lines under the page's filter:
// a tool shows when its name or any model matches; models narrow to the
// matching ones unless the tool itself matched. Tools that only have a quota
// follow the ones with usage.
func usageLines(rep protocol.UsageReport, filter string) []usageLine {
	var out []usageLine
	seen := map[string]bool{}
	add := func(t protocol.UsageToolRow, empty bool) {
		seen[t.Tool] = true
		toolHit := usageMatches(t.Tool, filter)
		var models []protocol.UsageModelRow
		for _, mr := range t.Models {
			if toolHit || usageMatches(mr.Model, filter) {
				models = append(models, mr)
			}
		}
		if !toolHit && len(models) == 0 {
			return
		}
		qs := usageQuotasFor(rep.Quotas, t.Tool, "")
		first := usageLine{kind: usageTool, name: t.Tool, row: t.UsageRow, empty: empty}
		if len(qs) > 0 {
			first.quota = &qs[0]
		}
		out = append(out, first)
		for i := 1; i < len(qs); i++ {
			out = append(out, usageLine{kind: usageExtraQuota, quota: &qs[i]})
		}
		for _, mr := range models {
			name := mr.Model
			if name == "" {
				name = "unknown"
			}
			l := usageLine{kind: usageModel, name: name, row: mr.UsageRow}
			if mqs := usageQuotasFor(rep.Quotas, t.Tool, mr.Model); len(mqs) > 0 && mr.Model != "" {
				l.quota = &mqs[0]
			}
			out = append(out, l)
		}
	}
	for _, t := range rep.Tools {
		add(t, false)
	}
	for _, q := range rep.Quotas {
		if !seen[q.Tool] && q.Model == "" {
			add(protocol.UsageToolRow{Tool: q.Tool}, true)
		}
	}
	return out
}

// usageTotal is the total row: the report's own, or the sum of what the
// filter shows.
func usageTotal(rep protocol.UsageReport, filter string) protocol.UsageRow {
	if filter == "" {
		return rep.Total
	}
	var t protocol.UsageRow
	var cost float64
	hasCost := false
	for _, tr := range rep.Tools {
		row := tr.UsageRow
		if !usageMatches(tr.Tool, filter) {
			row = protocol.UsageRow{}
			for _, mr := range tr.Models {
				if !usageMatches(mr.Model, filter) {
					continue
				}
				row.Sessions += mr.Sessions
				row.Tokens += mr.Tokens
				row.TokensEstimated = row.TokensEstimated || mr.TokensEstimated
				if mr.CostUSD != nil {
					row.CostUSD = ptrSum(row.CostUSD, *mr.CostUSD)
				}
			}
		}
		t.Sessions += row.Sessions
		t.Tokens += row.Tokens
		t.TokensEstimated = t.TokensEstimated || row.TokensEstimated
		if row.CostUSD != nil {
			cost += *row.CostUSD
			hasCost = true
		}
	}
	if hasCost {
		t.CostUSD = &cost
	}
	return t
}

func ptrSum(p *float64, v float64) *float64 {
	if p != nil {
		v += *p
	}
	return &v
}

// ---- cells -----------------------------------------------------------------

// usageTokens is a compact token count; "~" marks rex's own estimate.
func usageTokens(n int64, estimated bool) string {
	var s string
	switch {
	case n < 1000:
		s = fmt.Sprintf("%d", n)
	case n < 10_000:
		s = fmt.Sprintf("%.1fk", float64(n)/1e3)
	case n < 1_000_000:
		s = fmt.Sprintf("%dk", n/1000)
	case n < 10_000_000:
		s = fmt.Sprintf("%.1fM", float64(n)/1e6)
	default:
		s = fmt.Sprintf("%dM", n/1_000_000)
	}
	if estimated {
		return "~" + s
	}
	return s
}

func usageCost(c *float64) string {
	if c == nil {
		return "—"
	}
	return fmt.Sprintf("$%.2f", *c)
}

// usageWindow is the short label of a quota window.
func usageWindow(w string) string {
	switch w {
	case "week":
		return "wk"
	case "month":
		return "mo"
	}
	return truncate(w, 3)
}

// usageResetsIn is the time until a window resets: "2h10m", "42m", "4d".
func usageResetsIn(now, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := at.Sub(now)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// usageStale reports whether a reading is too old to trust.
func usageStale(now time.Time, q protocol.UsageQuota) bool {
	return q.Stale || (!q.At.IsZero() && now.Sub(q.At) > usageStaleAfter)
}

// quotaStyle colors a quota by use: success below 60%, warn below 85%,
// danger from there. A stale reading is dim whatever its value.
func quotaStyle(pct float64, stale bool) lipgloss.Style {
	switch {
	case stale:
		return styleDim
	case pct < 60:
		return styleQuotaOK
	case pct < 85:
		return styleQuotaWarn
	default:
		return styleQuotaDanger
	}
}

// usageBar is an n-cell bar for pct (0-100): filled ▰, empty ▱.
func usageBar(pct float64, n int) string {
	filled := int(pct/100*float64(n) + 0.5)
	filled = min(max(filled, 0), n)
	return strings.Repeat("▰", filled) + strings.Repeat("▱", n-filled)
}

const (
	usageBarCells  = 5
	usageQuotaW    = 14 // "5h ▰▰▱▱▱ 42%" and "mo $3/$20"
	usageResetsW   = 11 // "as of 14:02"
	usageSessionsW = 8
	usageTokensW   = 8
	usageCostW     = 9
)

// quotaCell is a quota's text, plain and styled; both are usageQuotaW wide.
func quotaCell(m Model, q *protocol.UsageQuota) (plain, styled string) {
	if q == nil {
		plain = usagePad("—", usageQuotaW, false)
		return plain, styleDim.Render(plain)
	}
	stale := usageStale(m.now(), *q)
	label := usagePad(usageWindow(q.Window), 2, false)
	body := "—"
	style := styleDim
	switch {
	case q.UsedPct != nil:
		body = usageBar(*q.UsedPct, usageBarCells) + fmt.Sprintf(" %3.0f%%", *q.UsedPct)
		style = quotaStyle(*q.UsedPct, stale)
	case q.UsedUSD != nil && q.LimitUSD != nil:
		body = fmt.Sprintf("$%.0f/$%.0f", *q.UsedUSD, *q.LimitUSD)
		if *q.LimitUSD > 0 {
			style = quotaStyle(*q.UsedUSD / *q.LimitUSD * 100, stale)
		}
	default:
	}
	plain = usagePad(label+" "+body, usageQuotaW, false)
	return plain, style.Render(plain)
}

// resetsCell is the time to the reset for a live reading and "as of HH:MM"
// for a stale one.
func resetsCell(m Model, q *protocol.UsageQuota) string {
	if q == nil {
		return ""
	}
	if usageStale(m.now(), *q) {
		if q.At.IsZero() {
			return styleDim.Render("stale")
		}
		return styleDim.Render("as of " + q.At.Format("15:04"))
	}
	return styleMuted.Render(usageResetsIn(m.now(), q.ResetsAt))
}

// padCells pads or cuts s to w cells; it right-aligns s when right is true.
func usagePad(s string, w int, right bool) string {
	s = ansi.Truncate(s, w, "…")
	gap := strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
	if right {
		return gap + s
	}
	return s + gap
}

// ---- layout ----------------------------------------------------------------

type usageLayout struct {
	box       []string
	maxScroll int
}

// usageInner is the table width on a w-column screen.
func usageInner(w int) int { return min(max(w-8, 30), 76) }

// usageTableMin is the narrowest inner width that holds the table.
const usageTableMin = 10 + 1 + usageSessionsW + 1 + usageTokensW + 1 + usageCostW + 1 + usageQuotaW + 1 + usageResetsW

// layoutUsage builds the page's box for a w x h screen and says how far its
// body scrolls.
func layoutUsage(m Model, w, h int) usageLayout {
	st := m.UsagePage
	if st == nil {
		st = &UsageState{Range: protocol.UsageRangeToday}
	}
	inner := usageInner(w)
	cards := mobileLayout(m) || inner < usageTableMin

	head := []string{usageRangeLine(st.Range)}
	switch {
	case st.Filtering:
		head = append(head, styleArrow.Render("/ ")+stylePrimary.Render(protocol.FlattenDisplayText(st.Filter))+cursorLit())
	case st.Filter != "":
		head = append(head, styleDim.Render("/ "+protocol.FlattenDisplayText(st.Filter)))
	}
	head = append(head, "")

	var body, foot []string
	switch {
	case st.Err != "":
		body = []string{styleArrowYellow.Render(truncate(st.Err, inner))}
	case st.Report == nil:
		body = []string{styleDim.Render("loading…")}
	default:
		body, foot = usageBody(m, *st.Report, st.Filter, inner, cards)
	}

	hints := [][2]string{{"←/→", "range"}, {"r", "refresh"}, {"/", "filter"}, {"~", "estimate"}, {"esc", "close"}}
	if inner < 56 {
		hints = [][2]string{{"←/→", "range"}, {"r", "refresh"}, {"/", "filter"}, {"esc", "close"}}
	}
	if st.Filtering {
		hints = [][2]string{{"enter", "keep"}, {"esc", "clear"}}
	}
	footer := renderHints(hints, inner)

	// Rows around the body: head, foot, blank, footer, two border rows.
	vh := max(h-len(head)-len(foot)-1-1-2, 3)
	maxScroll := max(len(body)-vh, 0)
	start := min(max(st.Scroll, 0), maxScroll)
	view := body
	if len(body) > vh {
		view = append([]string(nil), body[start:start+vh]...)
		if start > 0 {
			view[0] = styleDim.Render("↑ more")
		}
		if start+vh < len(body) {
			view[len(view)-1] = styleDim.Render("↓ more")
		}
	}
	content := append(append(append(append([]string{}, head...), view...), foot...), "", footer)
	right := "loading…"
	if !st.FetchedAt.IsZero() && !st.Loading {
		right = "updated " + durationAgoAt(m.now(), st.FetchedAt) + " ago"
	}
	box, _ := panelLines("USAGE", right, content, 2, 0, inner+6)
	if len(box) > h {
		box = append(box[:max(h-1, 1)], box[len(box)-1])
	}
	return usageLayout{box: box, maxScroll: maxScroll}
}

func usageRangeLine(cur string) string {
	parts := make([]string, len(usageRanges))
	for i, r := range usageRanges {
		if r == cur {
			parts[i] = styleKey.Render(r)
		} else {
			parts[i] = styleChipDim.Render(r)
		}
	}
	return strings.Join(parts, styleChipSep.Render(" · "))
}

// renderUsage returns the page box, fitting within w x h.
func renderUsage(m Model, w, h int) string {
	return strings.Join(layoutUsage(m, w, h).box, "\n")
}

// usageBody renders the scrolling lines and the pinned total of the report.
func usageBody(m Model, rep protocol.UsageReport, filter string, inner int, cards bool) (body, foot []string) {
	lines := usageLines(rep, filter)
	total := usageTotal(rep, filter)
	if len(lines) == 0 {
		if filter != "" {
			return []string{styleDim.Render("nothing matches " + protocol.FlattenDisplayText(filter))}, nil
		}
		return []string{styleDim.Render("No usage recorded in this range.")}, nil
	}
	rule := styleBorderFg.Render(strings.Repeat("─", inner))
	if cards {
		body = usageCards(m, lines, inner)
		return body, []string{rule, styleKey.Render("TOTAL") + " " + styleDim.Render(truncate(usageSummary(total), inner-6))}
	}
	nameW := inner - (1 + usageSessionsW + 1 + usageTokensW + 1 + usageCostW + 1 + usageQuotaW + 1 + usageResetsW)
	hdr := usagePad("AGENT / MODEL", nameW, false) + " " + usagePad("SESSIONS", usageSessionsW, true) + " " +
		usagePad("TOKENS", usageTokensW, true) + " " + usagePad("COST", usageCostW, true) + " " +
		usagePad("QUOTA", usageQuotaW, false) + " " + usagePad("RESETS", usageResetsW, false)
	body = append(body, styleDim.Render(hdr))
	for _, l := range lines {
		body = append(body, usageTableLine(m, l, nameW))
	}
	label := "TOTAL"
	if filter != "" {
		label = "TOTAL (filtered)"
	}
	foot = []string{rule, styleKey.Render(usagePad(label, nameW, false)) + " " + usageNumbers(total, false)}
	return body, foot
}

// usageNumbers is the sessions, tokens and cost cells.
func usageNumbers(r protocol.UsageRow, dim bool) string {
	s := usagePad(fmt.Sprintf("%d", r.Sessions), usageSessionsW, true) + " " +
		usagePad(usageTokens(r.Tokens, r.TokensEstimated), usageTokensW, true) + " " +
		usagePad(usageCost(r.CostUSD), usageCostW, true)
	if dim {
		return styleDim.Render(s)
	}
	return stylePrimary.Render(s)
}

func usageTableLine(m Model, l usageLine, nameW int) string {
	quota := strings.Repeat(" ", usageQuotaW)
	if l.quota != nil || l.kind == usageTool {
		_, quota = quotaCell(m, l.quota)
	}
	resets := resetsCell(m, l.quota)
	resets += strings.Repeat(" ", max(usageResetsW-ansi.StringWidth(resets), 0))
	switch l.kind {
	case usageExtraQuota:
		return strings.Repeat(" ", nameW+1+usageSessionsW+1+usageTokensW+1+usageCostW+1) + quota + " " + resets
	case usageModel:
		return styleDim.Render(usagePad("  "+protocol.FlattenDisplayText(l.name), nameW, false)) + " " +
			usageNumbers(l.row, true) + " " + quota + " " + resets
	default:
		nums := usageNumbers(l.row, false)
		if l.empty {
			dash := usagePad("—", usageSessionsW, true) + " " + usagePad("—", usageTokensW, true) + " " + usagePad("—", usageCostW, true)
			nums = styleDim.Render(dash)
		}
		return styleSlug.Render(usagePad(protocol.FlattenDisplayText(l.name), nameW, false)) + " " + nums + " " + quota + " " + resets
	}
}

// usageCards is the narrow layout: a card per tool (models under it, the
// quota lines last).
func usageCards(m Model, lines []usageLine, inner int) []string {
	var body, quotas []string
	flush := func() {
		body = append(body, quotas...)
		quotas = nil
	}
	for _, l := range lines {
		switch l.kind {
		case usageTool:
			flush()
			if len(body) > 0 {
				body = append(body, "")
			}
			body = append(body, styleSlug.Render(truncate(l.name, inner)))
			if !l.empty {
				body = append(body, "  "+styleDim.Render(truncate(usageSummary(l.row), inner-2)))
			}
		case usageModel:
			body = append(body, "  "+styleDim.Render(truncate(l.name+"  "+usageSummary(l.row), inner-2)))
		default:
		}
		if l.quota != nil {
			plain, styled := quotaCell(m, l.quota)
			reset := resetsCell(m, l.quota)
			line := "  " + styled
			if 2+ansi.StringWidth(plain)+1+ansi.StringWidth(reset) <= inner {
				line += " " + reset
			}
			quotas = append(quotas, line)
		}
	}
	flush()
	return body
}

func usageSummary(r protocol.UsageRow) string {
	return fmt.Sprintf("%d sess · %s · %s", r.Sessions, usageTokens(r.Tokens, r.TokensEstimated), usageCost(r.CostUSD))
}
