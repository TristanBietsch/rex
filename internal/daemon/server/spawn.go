package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/adapter"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/runtime/ids"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// unknownToolError is the bad_intent error for a tool/model pair the registry
// does not have. NewSession and Resume report it the same way.
func unknownToolError(toolID, modelID string) error {
	return badIntent(fmt.Errorf("tool %s/%s not in registry", toolID, modelID))
}

// handleNewSession validates p, spawns its session and returns the new
// session's ID. Errors from validation, and an unknown tool or model, are
// badIntent; the caller maps the rest to spawn_failed or too_many_sessions.
func handleNewSession(ctx context.Context, intentID string, p protocol.NewSession, srv *Server) (string, error) {
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
		return "", unknownToolError(p.ToolID, p.ModelID)
	}
	if p.Effort != "" && model.Effort != nil && !slices.Contains(model.Effort.Options, p.Effort) {
		return "", fmt.Errorf("effort %q not offered by %s/%s (options: %s)",
			p.Effort, p.ToolID, p.ModelID, strings.Join(model.Effort.Options, ", "))
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
	spec := spawnSpec{
		Tool:            tool,
		Model:           model,
		Effort:          p.Effort,
		CWD:             p.CWD,
		Trust:           srv.trustDecisionFor(tool, p.CWD, p.TrustFolder),
		SkipPermissions: srv.cfg.SkipPermissions != nil && srv.cfg.SkipPermissions(),
		HookFile:        hookFile,
		SessionID:       id,
	}
	// A tool that takes a preassigned conversation id (Claude's --session-id)
	// gets a fresh UUID, which is not the rex session id: the two diverge
	// after /clear or a resume anyway.
	resumeID := ""
	if tool.Resume != nil && len(tool.Resume.NewArgs) > 0 {
		resumeID = ids.NewSessionID()
		spec.Resume = &resumeArgs{ID: resumeID, New: true}
	}
	cmdArgs := buildArgv(spec)
	childEnv := childEnvFor(spec)

	ptyPrompt := p.InitialPrompt
	delivery := initialPromptDelivery(p.ToolID, cmdArgs, p.InitialPrompt)
	if delivery.argv != nil {
		cmdArgs = delivery.argv
		ptyPrompt = ""
		slog.Info("spawn: initial_prompt via argv", "tool", p.ToolID, "bytes", len(p.InitialPrompt))
	} else if ptyPrompt != "" {
		slog.Info("spawn: initial_prompt via pty paste", "tool", p.ToolID, "bytes", len(ptyPrompt), "plain", delivery.plainPaste)
	}

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
		StartedAt:   time.Now().UTC(),
		Fleet:       p.Fleet,
		ResumeID:    resumeID,
		TrustFolder: p.TrustFolder,
	}
	if err := cfg.Store.AddUnique(sess); err != nil {
		srv.ReleaseSession()
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
	HookFile        string
	SessionID       string
	// Resume, when set and the tool has a resume block, adds its resume or
	// new-session args (see resumeArgs).
	Resume *resumeArgs
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
// resume args, hook args. The prompt (`--` text or `--prompt-interactive=`)
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
	// Flags must precede the `--` prompt separator.
	if spec.Tool.Detect.Kind == "hooks" && spec.Tool.Detect.Format == "claude" {
		argv = append(argv, adapter.ClaudeHookArgs()...)
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
	return append(env, "REX_SESSION_ID="+spec.SessionID)
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
	resumeCapture, err := spec.Tool.Resume.CaptureRegexp() // nil-safe
	if err != nil {
		return err
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
		Adapter:        ad,
		InputCh:        inputCh,
		CompleteCh:     completeCh,
		InitialPrompt:  ptyPrompt,
		PlainPaste:     delivery.plainPaste,
		ReadySettle:    delivery.readySettle,
		SummaryRequest: cfg.SummaryRequest,
		TrustPrompt:    spec.Trust.Answer,
		ResumeCapture:  resumeCapture,
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
