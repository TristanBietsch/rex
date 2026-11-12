package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/adapter"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/skills"
	"github.com/tristanbietsch/rex/internal/runtime/ids"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// unknownToolError is the bad_intent error for a tool/model pair the registry
// does not have. NewSession and Resume report it the same way.
func unknownToolError(reg *registry.Registry, toolID, modelID string) error {
	if reg != nil && slices.Contains(reg.Disabled, toolID) {
		return badIntent(errors.New("off: enable in Settings → Features (`rex config set extra_agents true`)"))
	}
	return badIntent(fmt.Errorf("tool %s/%s not in registry", toolID, modelID))
}

// handleNewSession validates p, spawns its session and returns the new
// session's ID. Errors from validation, and an unknown tool or model, are
// badIntent; the caller maps the rest to spawn_failed or too_many_sessions.
func handleNewSession(ctx context.Context, intentID string, p protocol.NewSession, srv *Server) (string, error) {
	return spawnSession(ctx, intentID, p, srv, "", 0)
}

// spawnSession is handleNewSession with the loop that owns the session (""
// and 0 for an ordinary one), set on the session before it is announced.
func spawnSession(ctx context.Context, intentID string, p protocol.NewSession, srv *Server, loopID string, loopIter int) (string, error) {
	if err := validateNewSession(p); err != nil {
		slog.Warn("spawn: rejected", "req_id", intentID, "slug", p.Slug, "reason", err)
		return "", badIntent(err)
	}
	p, err := normalizeNewSession(p)
	if err != nil {
		slog.Warn("spawn: rejected", "req_id", intentID, "slug", p.Slug, "reason", err)
		return "", badIntent(err)
	}
	cfg := srv.cfg
	// Read the registry through the lock so SIGHUP-swapped versions take effect.
	tool, model, ok := srv.Registry().FindModel(p.ToolID, p.ModelID)
	if !ok {
		return "", unknownToolError(srv.Registry(), p.ToolID, p.ModelID)
	}
	if p.Effort != "" && model.Effort != nil && !slices.Contains(model.Effort.Options, p.Effort) {
		return "", fmt.Errorf("effort %q not offered by %s/%s (options: %s)",
			p.Effort, p.ToolID, p.ModelID, strings.Join(model.Effort.Options, ", "))
	}

	if len(p.Skills) > 0 {
		if err := srv.checkSkillsSupported(tool); err != nil {
			slog.Warn("spawn: rejected", "req_id", intentID, "slug", p.Slug, "reason", err)
			return "", badIntent(err)
		}
	}

	if !srv.TryAcquireSession() {
		return "", fmt.Errorf("too many concurrent sessions (cap=%d)", srv.MaxConcurrentSessions())
	}
	// From here, must release on error.

	id := ids.NewSessionID()
	short := ids.ExtendShortID(id, cfg.Store.TakenShortIDs())

	// Hook-detected tools report state by appending to a per-session file
	// named in the child's env.
	hookFile, err := state.HookFile(cfg.StateDir, id)
	if err != nil {
		srv.ReleaseSession()
		return "", fmt.Errorf("hook file: %w", err)
	}
	if tool.Detect.Kind == "hooks" {
		if _, err := state.CreateHookFile(cfg.StateDir, id); err != nil {
			srv.ReleaseSession()
			return "", fmt.Errorf("hook file: %w", err)
		}
		slog.Info("spawn: hook state detection", "tool", p.ToolID, "hook_file", hookFile)
	}
	// Chosen skills are copied into the session's own directory, so they
	// outlive edits to the library and go away with the session.
	pluginDir := ""
	if len(p.Skills) > 0 {
		sessDir, err := state.SessionDir(cfg.StateDir, id)
		if err == nil {
			pluginDir, err = skills.Stage(cfg.SkillsDir, sessDir, p.Skills)
		}
		if err != nil {
			srv.ReleaseSession()
			_ = state.RemoveSessionDir(cfg.StateDir, id)
			slog.Warn("spawn: rejected", "req_id", intentID, "slug", p.Slug, "reason", err)
			return "", badIntent(fmt.Errorf("skills: %w", err))
		}
		slog.Info("spawn: skills staged", "tool", p.ToolID, "skills", p.Skills)
	}
	spec := spawnSpec{
		Tool:            tool,
		Model:           model,
		Effort:          p.Effort,
		CWD:             p.CWD,
		PluginDir:       pluginDir,
		Trust:           srv.trustDecisionFor(tool, p.CWD, p.TrustFolder),
		SkipPermissions: srv.cfg.SkipPermissions != nil && srv.cfg.SkipPermissions(),
		HookFile:        hookFile,
		SessionID:       id,
		StatusLine:      srv.statusLineFor(tool, id),
		Cols:            p.Cols,
		Rows:            p.Rows,
	}
	// A tool that takes a preassigned conversation id (Claude's --session-id)
	// gets a fresh UUID, which is not the rex session id: the two diverge
	// after /clear or a resume anyway.
	resumeID := ""
	if srv.resumeOn() && tool.Resume != nil && len(tool.Resume.NewArgs) > 0 {
		resumeID = ids.NewSessionID()
		spec.Resume = &resumeArgs{ID: resumeID, New: true}
	}
	cmdArgs := buildArgv(spec)
	childEnv := childEnvFor(spec)

	ptyPrompt := p.InitialPrompt
	delivery := initialPromptDelivery(tool, cmdArgs, p.InitialPrompt)
	if delivery.argv != nil {
		cmdArgs = delivery.argv
		ptyPrompt = ""
		slog.Info("spawn: initial_prompt via argv", "tool", p.ToolID, "bytes", len(p.InitialPrompt))
	} else if ptyPrompt != "" {
		slog.Info("spawn: initial_prompt via pty paste", "tool", p.ToolID, "bytes", len(ptyPrompt), "plain", delivery.plainPaste)
	}

	// Creation is the session's first activity: last_event_at starts at
	// started_at so idle time is defined before any event arrives.
	created := time.Now().UTC()
	sess := &state.Session{
		ID:          id,
		ShortID:     short,
		ToolID:      p.ToolID,
		ModelID:     p.ModelID,
		Effort:      p.Effort,
		Slug:        p.Slug,
		Title:       p.Title,
		CWD:         p.CWD,
		State:       protocol.StateQueued,
		StartedAt:   created,
		LastEventAt: created,
		Fleet:       p.Fleet,
		ResumeID:    resumeID,
		TrustFolder: p.TrustFolder,
		Name:        p.Name,
		Skills:      slices.Clone(p.Skills),
		LoopID:      loopID,
		LoopIter:    loopIter,
	}
	if cfg.Audit != nil {
		cfg.Audit.Begin(sess, p.InitialPrompt)
	}
	if err := cfg.Store.AddUnique(sess); err != nil {
		srv.ReleaseSession()
		if pluginDir != "" {
			_ = state.RemoveSessionDir(cfg.StateDir, id)
		}
		var taken *state.SlugTakenError
		if errors.As(err, &taken) {
			slog.Warn("spawn: rejected, slug taken", "req_id", intentID, "slug", p.Slug, "holder", taken.ShortID)
			return "", badIntent(err)
		}
		return "", err
	}

	if err := startSupervisor(ctx, srv, sess, spec, cmdArgs, childEnv, ptyPrompt, delivery, nil); err != nil {
		srv.ReleaseSession()
		_ = cfg.Store.Remove(sess.ID)
		if pluginDir != "" {
			_ = state.RemoveSessionDir(cfg.StateDir, id)
		}
		return "", err
	}
	return id, nil
}

// spawnSpec is everything that decides a session's argv and environment.
// buildArgv and childEnvFor are pure over it, so argv is testable without a
// Server and a future Resume path can rebuild it from a persisted session.
type spawnSpec struct {
	Tool   registry.Tool
	Model  registry.Model
	Effort string
	CWD    string
	// Trust is the already-computed folder-trust decision (it reads config).
	Trust trustDecision
	// SkipPermissions adds Tool.SkipPermissionsArgs (setting skip_permissions,
	// read once per spawn) when Trust.Trusted.
	SkipPermissions bool
	// PluginDir is the staged skills plugin folder ("" = none). Added as the
	// tool's skill_args after the resume args and before the hook args.
	PluginDir string
	HookFile  string
	SessionID string
	// StatusLine is the statusLine command added to Claude's --settings (the
	// usage tap); "" adds none.
	StatusLine string
	// Resume, when set and the tool has a resume block, adds its resume or
	// new-session args (see resumeArgs).
	Resume *resumeArgs
	// Cols and Rows are the PTY's initial size (already clamped; 0 = the
	// supervisor's 120x32 default).
	Cols, Rows uint16
}

// resumeArgs asks buildArgv for the tool's resume.new_args (New: a fresh
// conversation whose id rex chose) or resume.args (resume that conversation),
// with {id} replaced by ID.
type resumeArgs struct {
	ID  string
	New bool
}

// buildArgv returns the child argv up to, but not including, the initial
// prompt: command, model args, effort, trust args, skip-permissions args,
// resume args, skill args, hook args. The prompt (`--` text or `--prompt-interactive=`)
// is appended by initialPromptDelivery.
func buildArgv(spec spawnSpec) []string {
	argv := append([]string{}, spec.Tool.Command...)
	argv = append(argv, spec.Model.Args...)
	// Apply the chosen reasoning effort by interpolating model.effort.arg_template.
	// We split the rendered template on whitespace so multi-token flags like
	// `-c key=value` work (each becomes its own argv slot).
	if spec.Model.Effort != nil && spec.Effort != "" && spec.Model.Effort.ArgTemplate != "" {
		rendered := strings.ReplaceAll(spec.Model.Effort.ArgTemplate, "{value}", spec.Effort)
		argv = append(argv, strings.Fields(rendered)...)
	}
	// Folder trust the agent applies itself (only under auto_trust_dirs).
	argv = append(argv, spec.Trust.Args...)
	// skip_permissions applies only in a trusted folder: elsewhere the agent
	// keeps asking, whatever the setting says.
	if spec.SkipPermissions && len(spec.Tool.SkipPermissionsArgs) > 0 {
		if spec.Trust.Trusted {
			argv = append(argv, spec.Tool.SkipPermissionsArgs...)
		} else {
			slog.Info("skip_permissions ignored: folder not trusted", "tool", spec.Tool.ID, "cwd", spec.CWD)
		}
	}
	// The id becomes argv, so only a canonical UUID gets through; callers
	// validate first, this is the backstop.
	if r := spec.Resume; r != nil && spec.Tool.Resume != nil && protocol.ValidResumeID(r.ID) {
		if r.New {
			argv = append(argv, spec.Tool.Resume.NewArgsFor(r.ID)...)
		} else {
			argv = append(argv, spec.Tool.Resume.ArgsFor(r.ID)...)
		}
	}
	if spec.PluginDir != "" {
		argv = append(argv, spec.Tool.SkillArgsFor(spec.PluginDir)...)
	}
	// Flags must precede the `--` prompt separator.
	if spec.Tool.Detect.Kind == "hooks" && spec.Tool.Detect.Format == "claude" {
		argv = append(argv, adapter.ClaudeSettingsArgs(spec.StatusLine)...)
	}
	return argv
}

// childEnvFor returns the extra environment for the child process.
func childEnvFor(spec spawnSpec) []string {
	var env []string
	if spec.Tool.Detect.Kind == "hooks" {
		// ToSlash: on Windows, Claude runs hooks in Git Bash, which reads
		// C:/... paths without backslash escapes. Go opens either form.
		env = append(env, adapter.HookFileEnv+"="+filepath.ToSlash(spec.HookFile))
	}
	// Appended after os.Environ, so status_env wins over a user's "off".
	if spec.Tool.Detect.Status != "" {
		env = append(env, spec.Tool.StatusEnv...)
	}
	return append(env, "REX_SESSION_ID="+spec.SessionID)
}

// statusDropEnv are the variables removed from the child's environment for a
// tool on the OSC 7501 channel: inside tmux (or a known terminal program) some
// agents never ask for the channel. The agent runs in rex's PTY, not tmux, so
// removing them is also the true environment.
var statusDropEnv = []string{"TMUX", "TMUX_PANE", "TERM_PROGRAM", "TERM_PROGRAM_VERSION"}

// childEnvDropFor returns the inherited variables to remove from the child.
func childEnvDropFor(spec spawnSpec) []string {
	if spec.Tool.Detect.Status == registry.StatusOSC7501 {
		return slices.Clone(statusDropEnv)
	}
	return nil
}

// startSupervisor builds the adapter and PTY supervisor for sess and runs it
// in the background. The caller owns the session slot and the Store entry:
// on error it must release the slot (and remove sess if it added it).
//
// res is the stop reservation a Resume holds (nil for a new session): it is
// swapped for the real stop func before the child is started, and if a Delete
// cancelled it first, startSupervisor returns errResumeCancelled and starts
// nothing, so a delete racing a resume leaves no process and no directory.
func startSupervisor(ctx context.Context, srv *Server, sess *state.Session, spec spawnSpec, argv, env []string, ptyPrompt string, delivery promptDelivery, res *stopReservation) error {
	cfg := srv.cfg
	ad, err := adapter.For(spec.Tool, spec.HookFile)
	if err != nil {
		return fmt.Errorf("build adapter: %w", err)
	}
	resumeOn := srv.resumeOn()
	var resumeCapture *regexp.Regexp
	if resumeOn {
		if resumeCapture, err = spec.Tool.Resume.CaptureRegexp(); err != nil { // nil-safe
			return err
		}
	}

	inputCh := make(chan []byte, 16)
	srv.RegisterInputChannel(sess.ID, inputCh)

	completeCh := make(chan struct{}, 1)

	sup := pty.New(pty.SupervisorConfig{
		StateDir:       cfg.StateDir,
		Store:          cfg.Store,
		Command:        argv,
		CWD:            spec.CWD,
		Env:            env,
		DropEnv:        childEnvDropFor(spec),
		InitialCols:    spec.Cols,
		InitialRows:    spec.Rows,
		Adapter:        ad,
		InputCh:        inputCh,
		CompleteCh:     completeCh,
		InitialPrompt:  ptyPrompt,
		PlainPaste:     delivery.plainPaste,
		ReadySettle:    delivery.readySettle,
		SummaryRequest: cfg.SummaryRequest,
		TrustPrompt:    spec.Trust.Answer,
		ResumeCapture:  resumeCapture,
		NoResumeIDs:    !resumeOn,
		OutputSink: func(b []byte) {
			srv.broadcastSessionOutput(sess.ID, b)
		},
		RegisterResize: func(fn func(cols, rows uint16) error) {
			srv.RegisterResize(sess.ID, fn)
		},
		UnregisterResize: func() {
			srv.UnregisterResize(sess.ID)
		},
		RegisterModePreamble: func(fn func() []byte) {
			srv.RegisterModePreamble(sess.ID, fn)
		},
		UnregisterModePreamble: func() {
			srv.UnregisterModePreamble(sess.ID)
		},
		RegisterWriter: func(w pty.UnitWriter) {
			srv.RegisterUnitWriter(sess.ID, w)
		},
		UnregisterWriter: func() {
			srv.UnregisterUnitWriter(sess.ID)
		},
		RegisterAttach: func(fn func(fn func(repaint func() []byte))) {
			srv.RegisterAttach(sess.ID, fn)
		},
		UnregisterAttach: func() {
			srv.UnregisterAttach(sess.ID)
		},
	})

	// Per-session ctx + done so IntentDelete can synchronously tear the PTY down.
	// The stop cause tells the supervisor this is a delete, not a failure.
	sessCtx, cancelCause := context.WithCancelCause(ctx)
	cancel := func() { cancelCause(nil) }
	done := make(chan struct{})
	stop := func() {
		cancelCause(pty.ErrSessionDeleted)
		<-done
	}
	if res != nil {
		if !srv.promoteStop(sess.ID, res, stop) {
			cancel()
			srv.UnregisterInputChannel(sess.ID)
			return errResumeCancelled
		}
	} else {
		srv.RegisterStop(sess.ID, stop)
	}
	srv.RegisterComplete(sess.ID, func() {
		select {
		case completeCh <- struct{}{}:
		default:
			// already pending; further signals are no-ops
		}
	})

	// Run in a background goroutine; the store events drive the wire.
	// Tracked in srv.sessions so Serve's shutdown waits for the supervisor's
	// final transition and meta write.
	srv.sessions.Add(1)
	go func() {
		defer srv.sessions.Done()
		defer close(done)
		// Release sessCtx on natural exit too; an uncanceled child context
		// stays registered with the server ctx for the daemon's lifetime.
		defer cancel()
		defer srv.UnregisterStop(sess.ID)
		defer srv.UnregisterComplete(sess.ID)
		defer srv.UnregisterInputChannel(sess.ID)
		defer srv.ReleaseSession()
		_ = sup.Run(sessCtx, sess)
	}()
	return nil
}

// checkSkillsSupported reports why skills cannot be loaded for tool, or nil.
func (s *Server) checkSkillsSupported(tool registry.Tool) error {
	switch {
	case !s.skillsOn():
		return errors.New("skills are off")
	case len(tool.SkillArgs) == 0:
		return fmt.Errorf("tool %s cannot load skills per session", tool.ID)
	case s.cfg.SkillsDir == "":
		return errors.New("no skills library configured")
	}
	return nil
}

// resumePlugin returns the plugin folder a resumed session loads, or "" for
// none. The folder staged at creation is reused as a snapshot; one that is
// gone is staged again from the library. With the feature off or a tool that
// cannot load skills nothing is added.
func (s *Server) resumePlugin(tool registry.Tool, id string, names []string) (string, error) {
	if len(names) == 0 || !s.skillsOn() || len(tool.SkillArgs) == 0 {
		return "", nil
	}
	sessDir, err := state.SessionDir(s.cfg.StateDir, id)
	if err != nil {
		return "", err
	}
	plugin := filepath.Join(sessDir, skills.PluginDirName)
	if fi, err := os.Lstat(plugin); err == nil && fi.IsDir() {
		return plugin, nil
	}
	if s.cfg.SkillsDir == "" {
		return "", errors.New("no skills library configured")
	}
	return skills.Stage(s.cfg.SkillsDir, sessDir, names)
}
