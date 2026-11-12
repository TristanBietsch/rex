package tui

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Focus is what currently has keyboard focus.
type Focus int

const (
	FocusBoard Focus = iota
	FocusPrompt
	FocusCommand
	FocusWizard
	FocusHelp
	FocusConfirmQuit
	FocusConfirmDelete
	FocusSettings
	FocusAttach
	FocusFail
	FocusBoot
	FocusStats
	FocusRename
	FocusSearch
	FocusOverview
	FocusLoops
	FocusConfirmLoopStop
	FocusUsage
	FocusUndo
)

// Model is the root Bubble Tea model.
type Model struct {
	Client *client.Client
	Socket string // path to daemon UDS, forwarded to child `rex attach`
	Focus  Focus
	// Width and Height are the layout area every view draws in. TermW and
	// TermH are the terminal size; the area is smaller by the window frame
	// when one is shown (frame.go). TermW is 0 for a model built without a
	// WindowSizeMsg (tests, rex render): then there is never a frame.
	Width, Height int
	TermW, TermH  int
	// BorderSeed seeds the "random" window border color: the launch folder.
	BorderSeed string
	// termLight is whether the terminal background was found to be light. Run
	// asks the terminal once, before Bubble Tea starts, and only when theme is
	// auto; otherwise (and in tests) it is false, the dark default. The auto
	// theme reads it: a later switch to auto reuses it and never asks again.
	termLight bool

	// WS is the workspace this board is fixed to (`rex here`); nil on an
	// ordinary board. wsTagging says the daemon tags sessions with their
	// workspace (else the board shows every session); wsNoticed that the
	// one notice about it was shown.
	WS        *WorkspaceScope
	wsTagging bool
	wsNoticed bool

	// Loops are the daemon's loops (Snapshot.Loops and Loop events); LoopsSel
	// is the selected row of the :loops overlay. PendingLoopStop is the loop
	// a stop confirmation asks about, loopStopBack the focus to return to.
	// looper overrides Client for the loop intents (tests).
	Loops           []protocol.LoopSummary
	LoopsSel        int
	PendingLoopStop string
	loopStopBack    Focus
	looper          loopController
	// borderRunOn and borderRunColor are :border's choices for this run. They
	// are never saved; nil / "" fall back to the window_border settings.
	borderRunOn    *bool
	borderRunColor string
	Sessions       []protocol.SessionSummary
	SelectedID     string
	// PendingSelectSlug is the slug of a session this TUI just asked the
	// daemon to spawn; its SessionAdded selects it.
	PendingSelectSlug string
	// PendingSpawn is what the wizard sent beyond what any daemon honors: a
	// daemon that predates the skills field drops it silently, so
	// SessionAdded of PendingSelectSlug is checked against it.
	PendingSpawn pendingSpawn
	Filter       string
	// Search is the board search text (/ or :search): every term must match a
	// row. Session-only; :bg saves it.
	Search string
	// searchOrig is Search when the editor opened; esc restores it.
	searchOrig string
	// Detailed shows the short id, model · effort and token columns and the
	// AI description (v toggles). The simple view shows the task.
	Detailed bool
	// DoneExpanded shows every Done row; a long Done column otherwise folds
	// into its title line (3 toggles).
	DoneExpanded bool
	// Hidden holds the IDs of board sections hidden from view. Treat it as
	// immutable: setSectionHidden is the only writer, and it clones.
	Hidden map[string]bool
	// EnteredAt is when this TUI saw each row enter Ready or Done; SeenAt is
	// when you last attached to it or pressed cc (saved). A row is unseen
	// while EnteredAt is after SeenAt. Nil until first written: use markSeen
	// and stampEntered, and assign the Model back.
	EnteredAt map[string]time.Time
	SeenAt    map[string]time.Time
	// seenInit is set once the first snapshot has seeded SeenAt.
	seenInit     bool
	PromptText   string
	CmdText      string
	PendingChord string
	Err          string
	// Notice is a transient, non-error status line message (cleared on the
	// next board key).
	Notice      string
	SpinnerTick int
	Quitting    bool

	Wizard   *WizardState
	Settings *SettingsState
	// Help is the help overlay's scroll and filter; nil reads as the top,
	// unfiltered.
	Help   *HelpState
	Attach *AttachState
	Fail   *FailState
	// UsagePage is the :usage page's state; set while FocusUsage.
	UsagePage *UsageState
	// Undo is the :undo / :redo dialog; set while FocusUndo.
	Undo   *UndoState
	Rename *RenameState
	// Audio is the active soundset player. Interface so tests can swap a recording fake.
	Audio audioPlayer

	// Clock overrides time.Now for rendering (tests). nil = wall clock.
	Clock func() time.Time
	// namer / retirer / deleter override Client for those intents (tests).
	namer   sessionNamer
	retirer sessionRetirer
	resumer sessionResumer
	deleter sessionDeleter
	// reloader sends :reload's Reload intent to the daemon on a socket.
	// nil means daemonctl.ReloadDaemon; tests swap it so no real daemon is
	// reached.
	reloader func(ctx context.Context, socket string) error

	// Store is the live settings store. Renderers read from it; the settings
	// page mutates it and triggers live-apply side effects.
	Store     *settings.Store
	StorePath string
	// Themes is the user's theme files (nil: built-in themes only). It is
	// replaced whole, on the UI goroutine, when the files are (re)loaded.
	Themes *theme.Catalog
	// configStamp is the state of config.yaml and the theme files as last
	// read; live reload compares against it. configTicking says the poll
	// timer chain is running.
	configStamp   string
	configTicking bool
	// luaLayer is what config.lua produced at the last settings load (zero
	// while lua_config is off or before boot).
	luaLayer settings.LayerReport

	// PendingDeleteID is the session targeted by an in-progress delete confirmation.
	PendingDeleteID string
	// PendingPrune holds the sessions a :prune confirmation would delete
	// (it shares the delete prompt); PendingPruneKept is how many it skips.
	PendingPrune     []string
	PendingPruneKept int

	// BlinkUntil tracks done-blink expiry per session.
	BlinkUntil map[string]time.Time

	// lastNotify is when each session last played a state-change sound
	// (debounce; see notify.go). Pruned on SessionRemoved.
	lastNotify map[string]time.Time
	// rungPrompt is the prompts count of the turn whose attention sound
	// already played, per session (see notify.go). Pruned on SessionRemoved.
	rungPrompt map[string]int

	// DescAnim is the active per-session description animation. Entries are
	// pruned by the descTickMsg handler once the animation completes.
	DescAnim        map[string]DescAnim
	descTickPending bool

	// spinnerGen / spinnerRunning track the single live spinner tick chain.
	spinnerGen     int
	spinnerRunning bool
	// bannerGen / bannerRunning track the attach banner's marquee tick chain.
	bannerGen     int
	bannerRunning bool
	// bannerHidden is ::banner: the attach banner is off for this run. Not
	// persisted; attach_banner is the setting.
	bannerHidden bool

	// exporting holds the sessions with a :export still running.
	exporting map[string]bool

	// reconnect is non-nil while the daemon connection is down and the TUI
	// is redialing it (see reconnect.go). dialDaemon overrides the redial's
	// handshakeDaemon (tests).
	reconnect  *reconnectState
	dialDaemon daemonDialer

	// BackendUnavailable mirrors the daemon's summarizer health flag, delivered
	// via SummarizerHealth events.
	BackendUnavailable       bool
	BackendUnavailableReason string

	// DaemonTools is the daemon's tool list (installed, merged models) from
	// the Snapshot and ToolsChanged; toolsKnown says it was sent at all (not
	// from a daemon that predates it: then every tool counts as installed).
	DaemonTools []protocol.ToolInfo
	toolsKnown  bool

	// Overview is the daemon's board overview (headline over the buckets the
	// TUI computes itself), from the Snapshot and Overview events. Nil
	// until the daemon sends one, and from daemons that predate it.
	Overview *protocol.Overview
	// OverviewSel is the selected row of the :overview overlay.
	OverviewSel int

	// Usage is the daemon's live usage quotas, from the Snapshot and Usage
	// events. Nil until the daemon sends one, and from daemons that predate it.
	Usage *protocol.Usage
	// usager answers the :usage page's UsageQuery (nil: Client); usageSeq
	// numbers its requests so a late reply for a closed page is dropped.
	usager   usageQuerier
	usageSeq int
	// undoer carries the :undo / :redo calls (nil: Client); undoSeq numbers
	// them so a late reply for a closed dialog is dropped.
	undoer undoDoer
	// spawner replaces Client for the quick-spawn's NewSession (tests).
	spawner sessionSpawner
	undoSeq int

	// ScrollOffset is how many board lines to skip from the top (for scroll).
	ScrollOffset int

	// mouseOn is whether rex last told the terminal to report mouse events
	// (see syncMouse). lastClickID / lastClickAt detect board double-clicks.
	mouseOn     bool
	lastClickID string
	lastClickAt time.Time

	// Screensaver (screensaver.go). lastInput is the last key or mouse
	// event; saverOn / saverSince say whether the screensaver shows and
	// since when. idleGen / idleArmed track the single idle timer chain,
	// which only runs when idleDone is set: Run closes idleDone on exit so
	// a pending timer ends with the program (hand-built test models leave
	// it nil, so they never start a long timer). keepAwake holds the
	// display on while the screensaver shows; nil does nothing.
	lastInput  time.Time
	saverOn    bool
	saverSince time.Time
	idleGen    int
	idleArmed  bool
	idleDone   <-chan struct{}
	keepAwake  *displayAwake

	// Boot / splash state.
	BootLog     []bootLine
	BootStep    int
	BootStart   time.Time
	BootMinDone bool
	BootDone    bool
	BootFailed  bool
	BootError   error
	// QuietBoot skips the boot chimes, startup tone and boot voice, for a
	// relaunch nobody is watching. Board sounds after boot still play.
	QuietBoot bool
}

// audioPlayer is the subset of *audio.Player the TUI uses.
type audioPlayer interface {
	Play(event string)
	SetVolume(v float64)
	SetEnabled(b bool)
	SetSoundset(name string)
	SetBootVoice(b bool)
}

func (m Model) applyEvent(env protocol.Envelope) Model {
	switch env.Type {
	case protocol.EventSessionAdded:
		var sum protocol.SessionSummary
		if err := json.Unmarshal(env.Data, &sum); err == nil {
			m = noteSessionWorkspace(m, sum)
			if i := sessionIndex(m.Sessions, sum.ID); i >= 0 {
				// Already listed (a snapshot taken after the add raced
				// its event): refresh the row, don't show it twice. It
				// may still be the spawn waiting to be selected.
				before := sectionOf(m, m.Sessions[i])
				m.Sessions[i] = sum
				stampEntered(&m, before, sum)
				m = m.selectIfPending(sum)
				break
			}
			m.Sessions = append(m.Sessions, sum)
			stampEntered(&m, "", sum)
			m = m.selectIfPending(sum)
			if m.Audio != nil && inScope(m, sum) {
				m.Audio.Play(audio.EventCreate)
			}
		}
	case protocol.EventSessionRemoved:
		var rem protocol.SessionRemoved
		if err := json.Unmarshal(env.Data, &rem); err == nil {
			endedMsg := "session deleted"
			if rem.Reason == protocol.RemovedRetired {
				endedMsg = "session cleared (marked done)"
			}
			// Another workspace's session leaves a scoped board silently.
			audible := m.wsFilterID() == ""
			if sum, ok := findSession(m.Sessions, rem.SessionID); ok {
				audible = inScope(m, sum)
			}
			m = dropTargets(m, rem.SessionID, endedMsg)
			if rem.Reason == protocol.RemovedRetired {
				if sum, ok := findSession(m.Sessions, rem.SessionID); ok {
					m.Notice = "cleared " + displayName(sum) + " (marked done)"
				}
			}
			if m.SelectedID == rem.SessionID {
				m.SelectedID = neighborOf(m, rem.SessionID)
			}
			out := m.Sessions[:0]
			for _, s := range m.Sessions {
				if s.ID != rem.SessionID {
					out = append(out, s)
				}
			}
			m.Sessions = out
			delete(m.lastNotify, rem.SessionID)
			delete(m.rungPrompt, rem.SessionID)
			delete(m.EnteredAt, rem.SessionID)
			delete(m.SeenAt, rem.SessionID)
			if m.Audio != nil && audible {
				m.Audio.Play(audio.EventDelete)
			}
		}
	case protocol.EventSessionUpdated:
		var upd protocol.SessionUpdated
		if err := json.Unmarshal(env.Data, &upd); err == nil {
			// prevState is the state before this patch; known is false for a
			// session this TUI never saw (no live transition to announce).
			var prevState protocol.State
			var prevReason string
			known, marked := false, false
			// audible is false for another workspace's session on a scoped
			// board: no blink, sound or notification for it.
			audible := m.wsFilterID() == ""
			for i := range m.Sessions {
				if m.Sessions[i].ID == upd.SessionID {
					known = true
					audible = inScope(m, m.Sessions[i])
					prevState = m.Sessions[i].State
					prevReason = m.Sessions[i].WaitReason
					prevDesc := m.Sessions[i].Description
					wasMarked := !m.Sessions[i].RetireAt.IsZero()
					beforeSection := sectionOf(m, m.Sessions[i])
					applyPatch(&m.Sessions[i], upd.Patch)
					stampEntered(&m, beforeSection, m.Sessions[i])
					marked = !wasMarked && !m.Sessions[i].RetireAt.IsZero()
					if m.Sessions[i].Description != prevDesc {
						m = registerDescAnim(m, m.Sessions[i].ID, prevDesc, m.Sessions[i].Description)
						if m.Attach != nil && m.Attach.SessionID == m.Sessions[i].ID {
							m.Attach.BannerSince = m.now() // read the new summary from its start
						}
					}
					break
				}
			}
			s, hasState := upd.Patch["state"].(string)
			next := protocol.State(s)
			// A repeated done patch is not a new completion; stay silent.
			completed := audible && hasState && next == protocol.StateDone && (!known || prevState != protocol.StateDone)
			if completed {
				if m.BlinkUntil == nil {
					m.BlinkUntil = make(map[string]time.Time)
				}
				m.BlinkUntil[upd.SessionID] = time.Now().Add(1600 * time.Millisecond)
			}
			// Covers needs_input → done too: one transition, one sound. Marking
			// done (cc, :done, rex done) leaves the session running, so no
			// state patch announces it; the mark itself plays the done sound,
			// once, even if the same patch also completes the session.
			if (completed || (marked && audible)) && m.Audio != nil {
				m.Audio.Play(audio.EventDone)
			}
			m = m.forgetStaleTurn(upd.SessionID)
			if audible && known && !hasState {
				m = m.notifyQuestion(upd.SessionID, prevReason)
			}
			if hasState && audible {
				m = m.notifyStateChange(upd.SessionID, prevState, next, known)
				if m.Undo != nil && m.Undo.StateAt != next {
					m = closeUndoIfStale(m, upd.SessionID, "undo: the session changed; run it again")
				}
			}
		}
	case protocol.EventSnapshot:
		var snap protocol.Snapshot
		if err := json.Unmarshal(env.Data, &snap); err == nil {
			m = adoptSessionsSeen(m, snap.Sessions)
			m.Overview = snap.Overview
			m.Usage = snap.Usage
			m = adoptSnapshotTools(m, snap.Tools)
			m = adoptWorkspaceTagging(m, &snap)
			m = adoptLoops(m, &snap)
			m = reselectIfGone(adoptSnapshotFilter(m, snap.Filter))
		}
	case protocol.EventToolsChanged:
		var tc protocol.ToolsChanged
		if err := json.Unmarshal(env.Data, &tc); err == nil {
			m = adoptDaemonTools(m, tc.Tools)
		}
	case protocol.EventUsage:
		var u protocol.Usage
		if err := json.Unmarshal(env.Data, &u); err == nil {
			m.Usage = &u
		}
	case protocol.EventLoop:
		var l protocol.LoopSummary
		if err := json.Unmarshal(env.Data, &l); err == nil && protocol.ValidLoopID(l.ID) {
			m = upsertLoop(m, l)
		}
	case protocol.EventOverview:
		var o protocol.Overview
		if err := json.Unmarshal(env.Data, &o); err == nil {
			m.Overview = &o
		}
	case protocol.EventError:
		var ee protocol.ErrorEvent
		if err := json.Unmarshal(env.Data, &ee); err == nil {
			slog.Warn("tui: daemon error event", "code", ee.Code, "message", ee.Message, "id", ee.ID)
			m.Err = "daemon: " + friendlyDaemonError(ee)
			// A rejected spawn will never produce SessionAdded.
			m.PendingSelectSlug = ""
			m.PendingSpawn = pendingSpawn{}
		}
	case protocol.EventSummarizerHealth:
		var h protocol.SummarizerHealth
		if err := json.Unmarshal(env.Data, &h); err == nil {
			m.BackendUnavailable = !h.Available
			m.BackendUnavailableReason = h.Reason
			slog.Info("tui: summarizer_health",
				"available", h.Available,
				"reason", h.Reason,
			)
		}
	}
	return m
}

func applyPatch(s *protocol.SessionSummary, patch map[string]any) {
	if v, ok := patch["state"].(string); ok {
		s.State = protocol.State(v)
	}
	if v, ok := patch["slug"].(string); ok {
		s.Slug = v
	}
	if v, ok := patch["title"].(string); ok {
		s.Title = v
	}
	if v, ok := patch["last_line"].(string); ok {
		s.LastLine = v
	}
	if v, ok := patch["description"].(string); ok {
		s.Description = v
	}
	if t, ok := patchTime(patch, "description_at"); ok {
		s.DescriptionAt = t
	}
	if v, ok := patch["fleet"].(string); ok {
		s.Fleet = v
	}
	if v, ok := patch["name"].(string); ok {
		s.Name = v
	}
	if t, ok := patchTime(patch, "last_event_at"); ok && !t.IsZero() {
		s.LastEventAt = t
	}
	if t, ok := patchTime(patch, "retire_at"); ok {
		s.RetireAt = t
	}
	if t, ok := patchTime(patch, "retired_at"); ok {
		s.RetiredAt = t
	}
	if t, ok := patchTime(patch, "ended_at"); ok {
		s.EndedAt = t
	}
	if t, ok := patchTime(patch, "working_since"); ok {
		s.WorkingSince = t
	}
	if v, present := patch["waiting_on"]; present {
		// null or "" clears. A kind this version doesn't know (a newer
		// daemon's) reads as not waiting: the session stays in Working.
		s.WaitingOn = ""
		if kind, _ := v.(string); protocol.ValidWaitKind(kind) {
			s.WaitingOn = kind
		}
	}
	if t, ok := patchTime(patch, "waiting_since"); ok {
		s.WaitingSince = t
	}
	// Token fields come as float64 over JSON; coerce safely.
	if v, ok := patch["tokens"].(float64); ok {
		s.Tokens = int64(v)
	}
	if v, ok := patch["output_bytes"].(float64); ok {
		s.OutputBytes = int64(v)
	}
	if v, ok := patch["subagents"].(float64); ok && v >= 0 {
		s.Subagents = int(v)
	}
	if raw, ok := patch["cost_usd"]; ok { // null clears it
		if v, isNum := raw.(float64); isNum && v >= 0 {
			s.CostUSD = &v
		} else {
			s.CostUSD = nil
		}
	}
	if v, ok := patch["prompts"].(float64); ok && v >= 0 {
		s.Prompts = int(v)
	}
	if raw, ok := patch["wait_reason"]; ok { // "" or null clears it
		v, _ := raw.(string)
		s.WaitReason = v
	}
	if v, ok := patch["resume_id"].(string); ok {
		s.ResumeID = v
	}
	if v, ok := patch["loop_id"].(string); ok {
		s.LoopID = v
	}
	if v, ok := patch["loop_iter"].(float64); ok && v >= 0 {
		s.LoopIter = int(v)
	}
	if v, present := patch["exit_code"]; present {
		switch code := v.(type) {
		case nil:
			s.ExitCode = nil // a resumed session starts over
		case float64:
			n := int(code)
			s.ExitCode = &n
		}
	}
}

// patchTime reads an optional timestamp patch key: an RFC3339 string sets
// it, null clears it (zero time). ok is false when the key is absent or
// unparseable.
func patchTime(patch map[string]any, key string) (time.Time, bool) {
	v, present := patch[key]
	if !present {
		return time.Time{}, false
	}
	if v == nil {
		return time.Time{}, true
	}
	str, isStr := v.(string)
	if !isStr {
		slog.Warn("tui: patch time not a string", "key", key)
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, str)
	if err != nil {
		slog.Warn("tui: patch time parse failed", "key", key, "err", err)
		return time.Time{}, false
	}
	return t, true
}

// friendlyDaemonError maps protocol errors to actionable status text.
func friendlyDaemonError(ee protocol.ErrorEvent) string {
	switch {
	case ee.Code == protocol.ErrCodeBadIntent && ee.Message == "intent not implemented":
		return "daemon is older than rex — run 'rex daemon restart'"
	case ee.Code == protocol.ErrCodeBadState && strings.HasPrefix(ee.ID, "unretire-"):
		return "too late — session is already being cleared"
	case strings.HasPrefix(ee.ID, "resume-") && ee.Code == protocol.ErrCodeBadState && ee.Message == protocol.MsgClearing:
		return "too late — session is already being cleared"
	case strings.HasPrefix(ee.ID, "resume-") && ee.Code == protocol.ErrCodeBadIntent && ee.Message == "resume is off":
		return offMessage("resume")
	}
	return ee.Message
}

// selectIfPending selects sum when it is the session this TUI just spawned
// (PendingSelectSlug), and reports spawn options the daemon ignored.
func (m Model) selectIfPending(sum protocol.SessionSummary) Model {
	if m.PendingSelectSlug == "" || sum.Slug != m.PendingSelectSlug {
		return m
	}
	m.SelectedID = sum.ID
	m.PendingSelectSlug = ""
	if msg := m.PendingSpawn.ignoredBy(sum); msg != "" {
		m.Err = msg
	}
	m.PendingSpawn = pendingSpawn{}
	if indexOfSelected(m) < 0 {
		m.Notice = sum.Slug + " is hidden by the filter"
	}
	return m
}

// sessionIndex is the index of session id in sessions, or -1.
func sessionIndex(sessions []protocol.SessionSummary, id string) int {
	for i := range sessions {
		if sessions[i].ID == id {
			return i
		}
	}
	return -1
}

// dropTargets closes whatever still points at session id once it is gone:
// the attach popup shows endedMsg, a rename or a delete confirmation for it
// closes. Without this a confirmation answered after another client deleted
// the session sent a delete for an id the daemon no longer knows.
func dropTargets(m Model, id, endedMsg string) Model {
	if m.Attach != nil && m.Attach.SessionID == id && !m.Attach.Ended {
		m.Attach.Ended = true
		m.Attach.EndedMsg = endedMsg
		slog.Info("tui: session removed while attached", "session", id, "msg", endedMsg)
	}
	if m.Rename != nil && m.Rename.SessionID == id {
		m.Rename = nil
		m.Focus = FocusBoard
		m.Err = "rename: session removed"
		slog.Info("tui: rename target removed", "session", id)
	}
	m = closeUndoIfStale(m, id, "undo: the session was removed")
	if m.PendingDeleteID != "" && m.PendingDeleteID == id {
		m.PendingDeleteID = ""
		if m.Focus == FocusConfirmDelete {
			m.Focus = FocusBoard
		}
		m.Notice = "already deleted"
		slog.Info("tui: delete target removed before confirm", "session", id)
	}
	return m
}

// adoptSessions replaces the session list with a fresh snapshot. Targets of
// sessions missing from it are dropped as if each got SessionRemoved.
func adoptSessions(m Model, sessions []protocol.SessionSummary) Model {
	for _, id := range []string{m.PendingDeleteID, m.renameTarget(), m.attachTarget(), m.undoTarget()} {
		if id != "" && sessionIndex(sessions, id) < 0 {
			m = dropTargets(m, id, "session deleted")
		}
	}
	if m.Undo != nil {
		if i := sessionIndex(sessions, m.Undo.SessionID); i >= 0 && sessions[i].State != m.Undo.StateAt {
			m = closeUndoIfStale(m, m.Undo.SessionID, "undo: the session changed; run it again")
		}
	}
	m.Sessions = sessions
	return m
}

// reselectIfGone moves a selection whose session left the list to the first
// row.
func reselectIfGone(m Model) Model {
	if sessionIndex(m.Sessions, m.SelectedID) >= 0 {
		return m
	}
	m.SelectedID = ""
	if rows := orderedSessions(m); len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return m
}

func (m Model) renameTarget() string {
	if m.Rename == nil {
		return ""
	}
	return m.Rename.SessionID
}

func (m Model) attachTarget() string {
	if m.Attach == nil {
		return ""
	}
	return m.Attach.SessionID
}

// neighborOf picks the row to select after id leaves the board: the next row
// in board order, else the previous one, else none.
func neighborOf(m Model, id string) string {
	rows := orderedSessions(m)
	for i, s := range rows {
		if s.ID != id {
			continue
		}
		if i+1 < len(rows) {
			return rows[i+1].ID
		}
		if i > 0 {
			return rows[i-1].ID
		}
	}
	return ""
}

// registerDescAnim records a description-change animation. Respects
// reduce_motion and the desc_animation setting.
func registerDescAnim(m Model, id, from, to string) Model {
	if to == "" {
		return m
	}
	var effect string
	if m.Store != nil {
		effect, _ = m.Store.Get("desc_animation").(string)
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			effect = "off"
		}
	}
	if effect == "" || effect == "off" {
		return m
	}
	if m.DescAnim == nil {
		m.DescAnim = map[string]DescAnim{}
	}
	if _, replacing := m.DescAnim[id]; replacing {
		slog.Info("desc_anim: replaced_in_flight", "session", id)
	}
	// Frames slice From/To by rune; sanitize first so an escape sequence
	// can't be cut in half mid-animation (cell() re-sanitizes each frame).
	m.DescAnim[id] = DescAnim{
		From: protocol.FlattenDisplayText(from), To: protocol.FlattenDisplayText(to), Effect: effect,
		StartedAt: time.Now(),
		Duration:  animDurationFor(effect),
	}
	slog.Debug("desc_anim: start",
		"session", id,
		"effect", effect,
		"duration_ms", animDurationFor(effect).Milliseconds(),
	)
	return m
}

func animDurationFor(effect string) time.Duration {
	switch effect {
	case "decode":
		return 400 * time.Millisecond
	case "wipe":
		return 250 * time.Millisecond
	default:
		return 300 * time.Millisecond
	}
}
