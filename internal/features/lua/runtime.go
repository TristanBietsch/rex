// Package lua provides the embedded Lua scripting runtime for rex.
//
// Users place an init.lua at the path configured by the lua_config_path
// setting (~/.config/rex/init.lua by default). The runtime loads that file
// once at startup and dispatches daemon events to any handlers registered via
// rex.on(). The embedded API is deliberately narrow: read session state, send
// text to a session's PTY, and log messages.
//
// Trust model: init.lua is trusted user code, like a shell rc file. It runs
// in-process with the daemon user's privileges. The os and io libraries are
// not loaded, but package/require, dofile, loadfile and load are, so a script
// can run any Lua file the user can read, and rex.send can type into any
// session. This is not a sandbox: never point lua_config_path at a script
// you did not write or review.
//
// Liveness: the top-level run of init.lua (Options.LoadTimeout) and every
// handler call (Options.HandlerTimeout) run under a deadline, and Close
// interrupts a call in flight. The deadlines keep a buggy script from
// stalling the daemon; they are not a security boundary. Memory use and time
// spent blocked inside a Go callback such as rex.send are not bounded by them.
//
// Threading model: *lua.LState is not goroutine-safe. All entry points
// (LoadFile, OnEvent, OnEventContext) are serialized via an internal mutex.
// Injected callbacks (Sender, Lister) are invoked while the mutex is held;
// they must not call back into the Runtime. The daemon never calls handlers
// from the store's broadcast path: a Dispatcher queues events and delivers
// them from one goroutine.
package lua

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ErrClosed is returned by Runtime calls made after Close.
var ErrClosed = errors.New("lua: runtime closed")

// Options configures a Runtime.
type Options struct {
	// Logger receives all runtime log output. If nil, slog.Default() is used.
	Logger *slog.Logger

	// Sender is called when Lua code invokes rex.send(session_id, text).
	// It is invoked with the Runtime mutex held; it must not re-enter the Runtime.
	Sender func(sessionID string, text string) error

	// Lister is called when Lua code invokes rex.list().
	// It is invoked with the Runtime mutex held; it must not re-enter the Runtime.
	Lister func() []protocol.SessionSummary

	// HandlerTimeout bounds each event handler call (0 = DefaultHandlerTimeout).
	HandlerTimeout time.Duration

	// LoadTimeout bounds the top-level execution of LoadFile (0 = DefaultLoadTimeout).
	LoadTimeout time.Duration
}

// Runtime wraps a gopher-lua LState and exposes the rex Lua API.
type Runtime struct {
	mu   sync.Mutex
	L    *lua.LState
	log  *slog.Logger
	opts Options
	// handlers maps event name -> list of *lua.LFunction registered via rex.on.
	handlers map[string][]*lua.LFunction

	// interruptMu guards interrupt and stopping. Close takes it, not mu, so
	// it can cancel a call that is still running (and holding mu) instead of
	// waiting it out.
	interruptMu sync.Mutex
	interrupt   context.CancelFunc // cancels the call in flight; nil when idle
	stopping    bool               // set by Close; later calls start canceled
	// closed is set under mu once L is closed; later calls return ErrClosed.
	closed bool
}

// New creates a new Runtime. The Lua state is initialized with the rex API
// table but no user script is loaded yet — call LoadFile for that.
func New(opts Options) (*Runtime, error) {
	if opts.Sender == nil {
		return nil, errors.New("lua.Options.Sender must not be nil")
	}
	if opts.Lister == nil {
		return nil, errors.New("lua.Options.Lister must not be nil")
	}
	if opts.HandlerTimeout <= 0 {
		opts.HandlerTimeout = DefaultHandlerTimeout
	}
	if opts.LoadTimeout <= 0 {
		opts.LoadTimeout = DefaultLoadTimeout
	}

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	// Open a minimal standard library. os/io are omitted to keep the API
	// surface small; this is not a sandbox (see the package doc).
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	for _, pair := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.LoadLibName, lua.OpenPackage},
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		if err := L.CallByParam(lua.P{
			Fn:      L.NewFunction(pair.fn),
			NRet:    0,
			Protect: true,
		}, lua.LString(pair.name)); err != nil {
			L.Close()
			return nil, fmt.Errorf("lua: open stdlib %q: %w", pair.name, err)
		}
	}

	r := &Runtime{
		L:        L,
		log:      log,
		opts:     opts,
		handlers: make(map[string][]*lua.LFunction),
	}

	registerAPI(r)

	return r, nil
}

// Close interrupts any call in flight, then releases the Lua state. It is
// safe to call more than once and from any goroutine; later LoadFile and
// OnEvent calls return ErrClosed.
func (r *Runtime) Close() {
	r.interruptMu.Lock()
	r.stopping = true
	if r.interrupt != nil {
		r.interrupt()
	}
	r.interruptMu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.L.Close()
}

// LoadFile loads and executes the Lua file at path. If the file does not exist
// the call is a no-op (logs once at info level and returns nil). A Lua syntax
// or runtime error is returned as a non-nil error, as is a top-level run that
// exceeds LoadTimeout (wrapping context.DeadlineExceeded). Handlers the
// script registered before failing stay registered.
func (r *Runtime) LoadFile(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		r.log.Info("lua: config file not found, skipping", "path", path)
		return nil
	}

	r.log.Debug("lua: loading config file", "path", path)
	err := r.runWithDeadline(context.Background(), r.opts.LoadTimeout, func() error {
		return r.L.DoFile(path)
	})
	if err != nil {
		r.log.Error("lua: error loading config file", "path", path, "timeout", r.opts.LoadTimeout, "err", err)
		return fmt.Errorf("lua: LoadFile %q: %w", path, err)
	}

	r.log.Info("lua: config file loaded", "path", path)
	return nil
}

// runWithDeadline runs call with a context installed on L that expires after
// timeout, or earlier when parent is canceled or Close starts. The VM checks
// the context before every instruction, so a runaway loop raises a Lua error
// at the deadline. A Go callback blocked inside call (rex.send) is not
// interrupted; it is bounded by its own timeout. An interrupted call's error
// wraps the context error (context.DeadlineExceeded or context.Canceled).
// Must be called with r.mu held.
func (r *Runtime) runWithDeadline(parent context.Context, timeout time.Duration, call func() error) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	r.armInterrupt(cancel)
	defer r.armInterrupt(nil)

	r.L.SetContext(ctx)
	defer r.L.RemoveContext()

	err := call()
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("interrupted after %s (%w): %w", timeout, ctx.Err(), err)
	}
	return err
}

// armInterrupt registers cancel as the way Close interrupts the call in
// flight (nil when the call ends). Once Close has started, cancel runs at
// once so the new call cannot outlive it.
func (r *Runtime) armInterrupt(cancel context.CancelFunc) {
	r.interruptMu.Lock()
	defer r.interruptMu.Unlock()
	r.interrupt = cancel
	if r.stopping && cancel != nil {
		cancel()
	}
}

// isStopping reports whether Close has started.
func (r *Runtime) isStopping() bool {
	r.interruptMu.Lock()
	defer r.interruptMu.Unlock()
	return r.stopping
}

// normaliseEventType maps protocol constants (e.g. "SessionAdded") to the
// lowercase Lua-facing names used as handler keys ("session_added").
func normaliseEventType(t string) string {
	switch t {
	case protocol.EventSessionAdded:
		return "session_added"
	case protocol.EventSessionUpdated:
		return "session_updated"
	case protocol.EventSessionRemoved:
		return "session_removed"
	default:
		return t
	}
}

// OnEvent is OnEventContext without a caller context: each handler call is
// still bounded by HandlerTimeout.
func (r *Runtime) OnEvent(eventType string, data any) error {
	return r.OnEventContext(context.Background(), eventType, data)
}

// OnEventContext dispatches an event to any Lua handlers registered via
// rex.on(). eventType accepts both protocol constants (e.g.
// protocol.EventSessionAdded) and the lowercase Lua-friendly names
// ("session_added"). data should be a protocol.SessionSummary for
// session_added/session_updated, or a protocol.SessionRemoved for
// session_removed.
//
// Each handler runs under HandlerTimeout and is interrupted when ctx is
// canceled or Close starts; the remaining handlers are then skipped. Lua
// errors and timeouts are logged and never propagated. The only error is
// ErrClosed, after Close.
func (r *Runtime) OnEventContext(ctx context.Context, eventType string, data any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}

	key := normaliseEventType(eventType)
	fns := r.handlers[key]
	if len(fns) == 0 {
		return nil
	}

	arg := r.dataToLua(data)

	for i, fn := range fns {
		r.log.Debug("lua: invoking handler", "event", key, "handler_index", i)
		err := r.runWithDeadline(ctx, r.opts.HandlerTimeout, func() error {
			return r.L.CallByParam(lua.P{
				Fn:      fn,
				NRet:    0,
				Protect: true,
			}, arg)
		})
		switch {
		case err == nil:
		case errors.Is(err, context.DeadlineExceeded):
			r.log.Warn("lua: handler timed out", "event", key, "handler_index", i, "timeout", r.opts.HandlerTimeout)
		default:
			r.log.Error("lua: handler error", "event", key, "handler_index", i, "err", err)
		}
		if ctx.Err() != nil || r.isStopping() {
			// Shutting down: the remaining handlers would be cut off at once.
			return nil
		}
		// Otherwise continue calling remaining handlers.
	}

	return nil
}

// dataToLua converts a supported Go value to a Lua value suitable for passing
// to an event handler. Must be called with r.mu held.
func (r *Runtime) dataToLua(data any) lua.LValue {
	switch v := data.(type) {
	case protocol.SessionSummary:
		return summaryToTable(r.L, v)
	case protocol.SessionRemoved:
		t := r.L.NewTable()
		r.L.SetField(t, "session_id", lua.LString(v.SessionID))
		return t
	default:
		return lua.LNil
	}
}

// summaryToTable converts a SessionSummary to a Lua table.
func summaryToTable(L *lua.LState, s protocol.SessionSummary) *lua.LTable {
	t := L.NewTable()
	L.SetField(t, "id", lua.LString(s.ID))
	L.SetField(t, "short_id", lua.LString(s.ShortID))
	L.SetField(t, "slug", lua.LString(s.Slug))
	L.SetField(t, "title", lua.LString(s.Title))
	L.SetField(t, "state", lua.LString(string(s.State)))
	L.SetField(t, "tool_id", lua.LString(s.ToolID))
	L.SetField(t, "model_id", lua.LString(s.ModelID))
	L.SetField(t, "cwd", lua.LString(s.CWD))
	L.SetField(t, "name", lua.LString(s.Name))
	if !s.RetireAt.IsZero() {
		L.SetField(t, "retire_at", lua.LNumber(s.RetireAt.Unix()))
	}
	if !s.RetiredAt.IsZero() {
		L.SetField(t, "retired_at", lua.LNumber(s.RetiredAt.Unix()))
	}
	return t
}
