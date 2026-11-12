package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Launching from the new-agent page (R16 "Launch and errors"): the page
// checks everything first (caps, every file and folder still inside the
// folder the agent starts in, the prompt limit of the chosen agent), applies
// the trust choice exactly as the wizard does, then sends NewSession and
// stays open with "⠋ starting…" until the daemon answers. Success closes
// the page, selects the new card and clears the draft; a failure shows on
// the page and keeps everything (D4).

// naLaunchTimeout bounds the wait for the daemon's answer to NewSession.
const naLaunchTimeout = 30 * time.Second

// naNoDaemon is the launch failure when there is no daemon to ask.
const naNoDaemon = "daemon is not running (rex daemon start)"

// naPromptHint follows a prompt over its limit.
const naPromptHint = "Save big pastes to a file in the workspace and import them (ctrl+o)."

// newAgentLaunchedMsg is the daemon's answer to the page's NewSession: Err
// nil once the session exists. gen tells this launch from an older one.
type newAgentLaunchedMsg struct {
	Slug string
	Err  error
	gen  int64
}

// launchGens numbers launches across pages, so an answer for a launch
// that was given up never lands on a newer one.
var launchGens atomic.Int64

// naSpawner is what a launch needs of the daemon client: Do waits for the
// answer, NewSession sends without one (a daemon older than acks).
type naSpawner interface {
	Do(ctx context.Context, intentType string, payload any) (protocol.Envelope, error)
	NewSession(req protocol.NewSession) error
}

// naSpawn sends req and waits for the daemon's answer. A daemon that
// predates acks gets the request the old way, and its answer (an error
// event, or the new card) arrives in the event stream as for the wizard.
func naSpawn(ctx context.Context, d naSpawner, req protocol.NewSession) error {
	if req.Slug == "" {
		req.Slug = "session"
	}
	_, err := d.Do(ctx, protocol.IntentNewSession, req)
	switch {
	case errors.Is(err, client.ErrNoAcks):
		return d.NewSession(req)
	case errors.Is(err, client.ErrClosed):
		return errors.New(naNoDaemon)
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("no answer from rex-daemon in " + naLaunchTimeout.String() + "; check the board before launching again")
	}
	return err
}

func naLaunchCmd(c *client.Client, req protocol.NewSession, gen int64) tea.Cmd {
	return func() tea.Msg {
		if c == nil {
			return newAgentLaunchedMsg{Slug: req.Slug, Err: errors.New(naNoDaemon), gen: gen}
		}
		ctx, cancel := context.WithTimeout(context.Background(), naLaunchTimeout)
		defer cancel()
		return newAgentLaunchedMsg{Slug: req.Slug, Err: naSpawn(ctx, c, req), gen: gen}
	}
}

// launchNewAgent checks the page and, when it can go, sends its NewSession
// and keeps the page open, starting. Anything that stops it is the page's
// error and nothing is sent.
func launchNewAgent(m Model) (Model, tea.Cmd) {
	st := m.NewAgent
	if st.Launching {
		return m, nil
	}
	cwd := spawnCWD(m)
	req, err := st.request(cwd, existingSlugs(m))
	if err != nil {
		st.Err = naFail(err)
		return m, nil
	}
	var ok bool
	if m, req, ok = newAgentTrustRequest(m, cwd, req); !ok {
		m.NewAgent.Err = "✗ " + m.Err // the page shows it, not the board
		m.Err = ""
		return m, nil
	}
	req.Cols, req.Rows = spawnSizeOf(m)
	m.PendingSelectSlug = req.Slug
	m.PendingSpawn = pendingSpawn{Skills: req.Skills}
	st.Launching = true
	st.launchGen = launchGens.Add(1)
	// The progress shows on Compose, wherever the launch came from.
	st.Screen, st.Filtering, st.Renaming, st.Complete, st.Import = naCompose, false, false, nil, nil
	return m, naLaunchCmd(m.Client, req, st.launchGen)
}

// naLaunching is the page or the draft (left with esc) whose launch gen is,
// or nil.
func naLaunching(m Model, gen int64) (st *NewAgentState, open bool) {
	if st := m.NewAgent; st != nil && st.Launching && st.launchGen == gen {
		return st, true
	}
	if d := m.newAgentDraft; d != nil && d.Launching && d.launchGen == gen {
		return d, false
	}
	return nil, false
}

// handleNewAgentLaunched takes the daemon's answer. Success closes the page
// (the new card is selected when it arrives, PendingSelectSlug), clears the
// draft and remembers the agent (D3). A failure keeps the page and all of
// the draft, and says so on the page, or on the board after esc.
func handleNewAgentLaunched(m Model, msg newAgentLaunchedMsg) (Model, tea.Cmd) {
	st, open := naLaunching(m, msg.gen)
	if st == nil {
		return m, nil // an answer for a launch given up
	}
	st.Launching = false
	if msg.Err != nil {
		m.PendingSelectSlug = ""
		m.PendingSpawn = pendingSpawn{}
		why := strings.TrimRight(protocol.FlattenDisplayText(msg.Err.Error()), ". ")
		if open {
			st.Err = "✗ launch failed: " + why + ". Your draft is kept."
		} else {
			m.Err = "launch failed: " + why + ". Your draft is kept (n reopens it)."
		}
		return m, nil
	}
	c := st.choice()
	m.lastSpawn = &c
	if open {
		return closeNewAgent(m, false), nil
	}
	m.newAgentDraft = nil
	return m, nil
}

// naLaunchingKey is a key while the launch waits for the daemon: esc
// closes the page and keeps the draft; nothing else does anything.
func naLaunchingKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if k.Type == tea.KeyEsc {
		return closeNewAgent(m, true), nil
	}
	return m, nil
}

// naStartingLine is the launch's progress row (mockup F).
func naStartingLine(m Model) string {
	return styleArrow.Render(naSpinner(m)) + styleDim.Render(" starting…")
}

// naCheckLaunch checks the draft for a launch from cwd: the item cap,
// every file and folder against the path rules again (it may have changed
// since it was added, or the draft was made for another folder), and the
// paste caps. Every error is in the page's words and names the item.
func (st *NewAgentState) naCheckLaunch(cwd string) error {
	c := spawnctx.DefaultCaps
	if len(st.Items) > c.Items {
		return naErr{msg: fmt.Sprintf("%d context items; the limit is %d: remove one first.", len(st.Items), c.Items)}
	}
	for _, it := range st.Items {
		if it.Kind != spawnctx.File && it.Kind != spawnctx.Dir {
			continue
		}
		if st.ItemsRoot != "" && filepath.Clean(st.ItemsRoot) != filepath.Clean(cwd) {
			return naErr{msg: it.Name + " was added in " + tildePath(st.ItemsRoot) + "; new agents now start in " +
				tildePath(cwd) + ". Remove it (x) to launch here."}
		}
		info, err := spawnctx.ValidatePath(cwd, it.Name)
		if err != nil {
			return naLaunchPathErr(cwd, it.Name, err)
		}
		if info.Dir != (it.Kind == spawnctx.Dir) {
			now := "file"
			if info.Dir {
				now = "folder"
			}
			return naErr{msg: it.Name + " is now a " + now + ": remove it (x) and add it again."}
		}
	}
	total := 0
	for _, it := range st.Items {
		if it.Kind != spawnctx.Paste {
			continue
		}
		size := len(strings.TrimRight(spawnctx.Sanitize(it.Text), "\n"))
		if size > c.Paste {
			return naErr{tooBig: true, msg: fmt.Sprintf("%s is %s; the limit for one paste is %s.", it.Name, naBytes(size), naBytes(c.Paste))}
		}
		if total += size; total > c.PasteTotal {
			return naErr{tooBig: true, msg: fmt.Sprintf("pasted text is %s in all; the limit is %s in total.", naBytes(total), naBytes(c.PasteTotal))}
		}
	}
	return nil
}

// naLaunchPathErr is a file or folder refused at launch (mockup F words).
func naLaunchPathErr(root, rel string, err error) error {
	switch {
	case errors.Is(err, spawnctx.ErrOutside):
		return naPathErr(root, rel, err)
	case errors.Is(err, spawnctx.ErrSpecial):
		return naErr{msg: rel + " is no longer a regular file or folder: remove it (x)."}
	case errors.Is(err, spawnctx.ErrBadName):
		return naErr{msg: strconv.Quote(rel) + " has control characters or line breaks in its name: remove it (x)."}
	case errors.Is(err, fs.ErrNotExist):
		return naErr{msg: rel + " does not exist (any more). Remove it (x)."}
	}
	return naErr{msg: "cannot check " + rel + ": " + err.Error()}
}

// naPromptErr is the assembled prompt over limit, in KB; tool names the
// agent when the limit is its own (a prompt_paste agent).
func naPromptErr(size, limit int, tool string) error {
	was := "is " + naBytes(size)
	if naBytes(size) == naBytes(limit) {
		was = "is just over " + naBytes(limit)
	}
	msg := "the prompt " + was + "; the limit is " + naBytes(limit) + "."
	if tool != "" {
		msg = "the prompt " + was + "; " + tool + " takes it as a terminal paste and the limit for that is " +
			naBytes(limit) + "."
	}
	return naErr{msg: msg, tooBig: true, hint: naPromptHint}
}

// naAssembleErr is an item Assemble refused, in the page's words.
func naAssembleErr(err error) error {
	if errors.Is(err, spawnctx.ErrBadItem) {
		why := strings.TrimSuffix(err.Error(), ": "+spawnctx.ErrBadItem.Error())
		return naErr{msg: "cannot launch with " + why + ": rename or remove it."}
	}
	return naErr{msg: "cannot launch: " + err.Error()}
}

// closeNewAgentIfOff closes the page, keeping the draft, once
// new_agent_page is off (Feature flag: turning it off closes the page).
func closeNewAgentIfOff(m Model) Model {
	if m.NewAgent != nil && !m.featureOn("new_agent_page") {
		return closeNewAgent(m, true)
	}
	return m
}

// naSeedWizard starts the older wizard (new_agent_page off) with the draft's
// task as one line; the context cannot go with it and the notice says so.
func naSeedWizard(m Model) Model {
	d := m.newAgentDraft
	if d == nil || m.Wizard == nil {
		return m
	}
	m.Wizard.TaskText = strings.Join(strings.Fields(d.Task.String()), " ")
	if n := len(d.Items); n > 0 {
		what := strconv.Itoa(n) + " context items were dropped"
		if n == 1 {
			what = "1 context item was dropped"
		}
		m.Notice = "the older wizard takes the task text only: " + what + " (kept for the new-agent page)"
	}
	return m
}
