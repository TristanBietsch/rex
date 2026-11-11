package inspect

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunOverview prints what needs you, what is ready for review, what failed,
// what is working and what finished: the daemon's one-line headline (when
// overview_enabled is on) over buckets computed here from the sessions.
// Exit code 0.
func RunOverview(args []string) error {
	fs := flag.NewFlagSet("overview", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.HelloFailed(err)
	}
	p := core.Out()
	return p.Emit(overviewResult(snap, p.Now(), p.Glyphs))
}

// overviewData is the `-o json` payload: the daemon's overview (null from a
// daemon that has none) and the full ids in each bucket, in the same order
// as the text view. Completed lists only the last 24 hours.
type overviewData struct {
	Overview *protocol.Overview  `json:"overview"`
	Buckets  map[string][]string `json:"buckets"`
}

// overviewTitles are the section titles in the human formats.
var overviewTitles = map[overview.Bucket]string{
	overview.Review:    "Ready for review",
	overview.NeedsYou:  "Needs you",
	overview.Failed:    "Failed",
	overview.Working:   "Working",
	overview.Completed: "Completed (last 24 h)",
}

// overviewCountLabels name the buckets in the counts line.
var overviewCountLabels = map[overview.Bucket]string{
	overview.Review:    "ready for review",
	overview.NeedsYou:  "needs you",
	overview.Failed:    "failed",
	overview.Working:   "working",
	overview.Completed: "completed",
}

// overviewOrder lists the buckets as every view shows them: what to look at
// first.
var overviewOrder = []overview.Bucket{overview.Review, overview.NeedsYou, overview.Failed, overview.Working, overview.Completed}

func overviewResult(snap *protocol.Snapshot, now time.Time, g core.Glyphs) core.Result {
	groups := overview.Group(snap.Sessions)
	data := overviewData{Buckets: map[string][]string{}}
	if o := snap.Overview; o != nil {
		cp := *o
		cp.Headline = protocol.SanitizeDisplayText(cp.Headline, protocol.MaxOverviewRunes)
		data.Overview = &cp
	}
	listed := map[overview.Bucket][]protocol.SessionSummary{}
	for _, b := range overviewOrder {
		ids := []string{}
		for _, s := range groups[b] {
			if overview.Listed(b, s, now) {
				listed[b] = append(listed[b], s)
				ids = append(ids, s.ID)
			}
		}
		data.Buckets[b.Key()] = ids
	}
	return core.Result{Cmd: "overview", Data: data, View: overviewView(data.Overview, listed, now, g)}
}

// overviewView builds the human and agent layout: a header, the headline, the
// counts, then one table per non-empty bucket.
func overviewView(o *protocol.Overview, listed map[overview.Bucket][]protocol.SessionSummary, now time.Time, g core.Glyphs) core.View {
	meta := ""
	hasHeadline := o != nil && o.Enabled && o.Headline != ""
	if hasHeadline && !o.GeneratedAt.IsZero() {
		meta = "updated " + core.DurationAgoAt(o.GeneratedAt, now)
	}
	v := core.View{core.Header{Title: "Overview", Meta: meta}}
	switch {
	case hasHeadline:
		pairs := []core.Pair{{Key: "headline", Label: "Headline", Val: core.Cell{Text: o.Headline}}}
		if !o.GeneratedAt.IsZero() {
			pairs = append(pairs, core.Pair{Key: "generated_at", Label: "Updated", Formats: core.SetOf(core.FormatAgent),
				Val: core.Cell{Text: o.GeneratedAt.UTC().Format(time.RFC3339), Raw: o.GeneratedAt.UTC().Format(time.RFC3339)}})
		}
		v = append(v, core.KV{Kind: "overview", Pairs: pairs})
	case o != nil && o.Enabled:
		v = append(v, core.Note{Tone: core.ToneDim, Text: "no headline yet"})
	default:
		v = append(v, core.Note{Tone: core.ToneDim, Text: "no headline: overview is off (rex config set overview_enabled true)"})
	}

	counts := core.KV{Inline: true}
	var txt []string
	for _, b := range overviewOrder {
		n := len(listed[b])
		counts.Pairs = append(counts.Pairs, core.Pair{Key: b.Key(), Label: overviewCountLabels[b],
			Val: core.Cell{Text: fmt.Sprint(n), Raw: n, Tone: bucketTone(b)}})
		txt = append(txt, fmt.Sprintf("%d %s", n, b.Key()))
	}
	counts.Txt = strings.Join(txt, " · ")
	v = append(v, counts)

	text, txtF, md := core.SetOf(core.FormatText), core.SetOf(core.FormatTXT), core.SetOf(core.FormatMD)
	agent := core.SetOf(core.FormatAgent)
	for _, b := range overviewOrder {
		list := listed[b]
		if len(list) == 0 {
			continue
		}
		cols := []core.Col{
			{Key: "mark", Title: "", Formats: text},
			{Key: "bucket", Title: "Bucket", Formats: agent},
			{Key: "id", Title: "ID", Width: 5},
			{Key: "name", Title: "Name", Width: 24, Max: 24, Flex: true, Min: 8},
			{Key: "state", Title: "State", Formats: txtF | md | agent},
			{Key: "status", Title: "Status", Flex: true, Min: 12},
			{Key: "age", Title: "Age", Align: core.AlignRight, Formats: text | txtF | md},
			{Key: "age_s", Title: "Age s", Formats: agent},
		}
		rows := make([][]core.Cell, 0, len(list))
		for _, s := range list {
			name := s.Name
			if name == "" {
				name = s.Slug
			}
			at := overview.At(s)
			ageS := -1
			if !at.IsZero() {
				ageS = int(now.Sub(at).Seconds())
			}
			tone := bucketTone(b)
			rows = append(rows, []core.Cell{
				{Text: g.State[s.State], Tone: tone},
				{Text: b.Key()},
				{Text: s.ShortID},
				{Text: name},
				{Text: string(s.State), Raw: string(s.State)},
				{Text: overviewStatus(s)},
				{Text: strings.TrimSuffix(core.DurationAgoAt(at, now), " ago"), Tone: core.ToneDim},
				{Text: fmt.Sprint(ageS), Raw: ageS},
			})
		}
		v = append(v, core.Section{Title: overviewTitles[b]}, core.Table{Kind: "session", Cols: cols, Rows: rows})
	}
	if len(listed[overview.Review])+len(listed[overview.NeedsYou])+len(listed[overview.Failed])+len(listed[overview.Working])+len(listed[overview.Completed]) > 0 {
		v = append(v, core.Hint{Cmd: "rex attach <id>", Text: "open a session"})
	}
	return v
}

// overviewStatus is a session's latest status: the question it waits on,
// else its description, else its task.
func overviewStatus(s protocol.SessionSummary) string {
	if s.State == protocol.StateNeedsInput && overview.IsQuestion(s.LastLine) {
		return strings.TrimSpace(s.LastLine)
	}
	if s.Description != "" {
		return s.Description
	}
	return s.Title
}

func bucketTone(b overview.Bucket) core.Tone {
	switch b {
	case overview.NeedsYou:
		return core.ToneNeeds
	case overview.Review, overview.Completed:
		return core.ToneDone
	case overview.Failed:
		return core.ToneFailed
	case overview.Working:
		return core.ToneWorking
	}
	return core.ToneNone
}
