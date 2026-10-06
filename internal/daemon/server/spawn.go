package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/adapter"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/runtime/ids"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func handleNewSession(ctx context.Context, intentID string, p protocol.NewSession, srv *Server, w *protocol.Writer) error {
	if err := validateNewSession(p); err != nil {
		return err
	}
	cfg := srv.cfg
	// Read the registry through the lock so SIGHUP-swapped versions take effect.
	tool, model, ok := srv.Registry().FindModel(p.ToolID, p.ModelID)
	if !ok {
		return fmt.Errorf("tool %s/%s not in registry", p.ToolID, p.ModelID)
	}

	if !srv.TryAcquireSession() {
		return fmt.Errorf("too many concurrent sessions (cap=%d)", srv.MaxConcurrentSessions())
	}
	// From here, must release on error.

	id := ids.NewSessionID()
	short := ids.ExtendShortID(id, cfg.Store.TakenShortIDs())

	cmdArgs := append([]string{}, tool.Command...)
	cmdArgs = append(cmdArgs, model.Args...)
	// Apply the chosen reasoning effort by interpolating model.effort.arg_template.
	// We split the rendered template on whitespace so multi-token flags like
	// `-c key=value` work (each becomes its own argv slot).
	if model.Effort != nil && p.Effort != "" && model.Effort.ArgTemplate != "" {
		rendered := strings.ReplaceAll(model.Effort.ArgTemplate, "{value}", p.Effort)
		cmdArgs = append(cmdArgs, strings.Fields(rendered)...)
	}

	// Hook-detected tools report state by appending to a per-session file
	// named in the child's env. Flags must precede the `--` prompt separator.
	var childEnv []string
	hookFile := state.HookFile(cfg.StateDir, id)
	if tool.Detect.Kind == "hooks" {
		if err := os.MkdirAll(filepath.Dir(hookFile), 0o755); err != nil {
			srv.ReleaseSession()
			return fmt.Errorf("hook dir: %w", err)
		}
		switch tool.Detect.Format {
		case "claude":
			cmdArgs = append(cmdArgs, adapter.ClaudeHookArgs()...)
		}
		childEnv = append(childEnv, adapter.HookFileEnv+"="+hookFile)
		slog.Info("spawn: hook state detection", "tool", p.ToolID, "hook_file", hookFile)
	}
	childEnv = append(childEnv, "REX_SESSION_ID="+id)

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
		ID:        id,
		ShortID:   short,
		ToolID:    p.ToolID,
		ModelID:   p.ModelID,
		Effort:    p.Effort,
		Slug:      p.Slug,
		Title:     p.Title,
		CWD:       p.CWD,
		State:     protocol.StateQueued,
		StartedAt: time.Now().UTC(),
		Fleet:     p.Fleet,
	}
	if err := cfg.Store.Add(sess); err != nil {
		srv.ReleaseSession()
		return err
	}

	ad, err := adapter.For(tool, hookFile)
	if err != nil {
		srv.ReleaseSession()
		_ = cfg.Store.Remove(sess.ID)
		return fmt.Errorf("build adapter: %w", err)
	}

	inputCh := make(chan []byte, 16)
	srv.RegisterInputChannel(sess.ID, inputCh)

	completeCh := make(chan struct{}, 1)

	sup := pty.New(pty.SupervisorConfig{
		StateDir:       cfg.StateDir,
		Store:          cfg.Store,
		Command:        cmdArgs,
		CWD:            p.CWD,
		Env:            childEnv,
		Adapter:        ad,
		InputCh:        inputCh,
		CompleteCh:     completeCh,
		InitialPrompt:  ptyPrompt,
		PlainPaste:     delivery.plainPaste,
		ReadySettle:    delivery.readySettle,
		SummaryRequest: cfg.SummaryRequest,
		OutputSink: func(b []byte) {
			srv.broadcastSessionOutput(sess.ID, b)
		},
		RegisterResize: func(fn func(cols, rows uint16) error) {
			srv.RegisterResize(sess.ID, fn)
		},
		UnregisterResize: func() {
			srv.UnregisterResize(sess.ID)
		},
	})

	// Per-session ctx + done so IntentDelete can synchronously tear the PTY down.
	sessCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	srv.RegisterStop(sess.ID, func() {
		cancel()
		<-done
	})
	srv.RegisterComplete(sess.ID, func() {
		select {
		case completeCh <- struct{}{}:
		default:
			// already pending; further signals are no-ops
		}
	})

	// Run in a background goroutine; the store events drive the wire.
	go func() {
		defer close(done)
		defer srv.UnregisterStop(sess.ID)
		defer srv.UnregisterComplete(sess.ID)
		defer srv.UnregisterInputChannel(sess.ID)
		defer srv.ReleaseSession()
		_ = sup.Run(sessCtx, sess)
	}()
	_ = intentID
	_ = w
	return nil
}
