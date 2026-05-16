// Package lua provides a sandboxed Lua scripting runtime for rex.
//
// Users place an init.lua at the path configured by the lua_config_path
// setting (~/.config/rex/init.lua by default). The runtime loads that file
// once at startup and dispatches daemon events to any handlers registered via
// rex.on(). The embedded API is deliberately narrow: read session state, send
// text to a session's PTY, and log messages.
//
// Threading model: *lua.LState is not goroutine-safe. All entry points
// (LoadFile, OnEvent) are serialized via an internal mutex. Injected callbacks
// (Sender, Lister) are invoked while the mutex is held; they must not call
// back into the Runtime.
//
// Security: the script runs in-process as the daemon user. Do not load
// untrusted scripts. The standard Lua os/io/file libraries are not loaded,
// but the script can still call any registered Go callback.
package lua

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	lua "github.com/yuin/gopher-lua"

	"github.com/tristanbietsch/rex/internal/protocol"
)

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
}

// Runtime wraps a gopher-lua LState and exposes the rex Lua API.
type Runtime struct {
	mu      sync.Mutex
	L       *lua.LState
	log     *slog.Logger
	opts    Options
	// handlers maps event name -> list of *lua.LFunction registered via rex.on.
	handlers map[string][]*lua.LFunction
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

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	// Open only the safe standard libraries; omit os/io to reduce attack surface.
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

// Close releases the Lua state.
func (r *Runtime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.L.Close()
}

// LoadFile loads and executes the Lua file at path. If the file does not exist
// the call is a no-op (logs once at info level and returns nil). A Lua syntax
// or runtime error is returned as a non-nil error.
func (r *Runtime) LoadFile(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		r.log.Info("lua: config file not found, skipping", "path", path)
		return nil
	}

	slog.Debug("lua: loading config file", "path", path)
	if err := r.L.DoFile(path); err != nil {
		r.log.Error("lua: error loading config file", "path", path, "err", err)
		return fmt.Errorf("lua: LoadFile %q: %w", path, err)
	}

	r.log.Info("lua: config file loaded", "path", path)
	return nil
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

// OnEvent dispatches an event to any Lua handlers registered via rex.on().
// eventType accepts both protocol constants (e.g. protocol.EventSessionAdded)
// and the lowercase Lua-friendly names ("session_added").
// data should be a protocol.SessionSummary for session_added/session_updated,
// or a protocol.SessionRemoved for session_removed.
//
// Lua errors are caught, logged at error level, and never propagated.
func (r *Runtime) OnEvent(eventType string, data any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := normaliseEventType(eventType)
	fns := r.handlers[key]
	if len(fns) == 0 {
		return nil
	}

	arg := r.dataToLua(data)

	for i, fn := range fns {
		r.log.Debug("lua: invoking handler", "event", key, "handler_index", i)
		if err := r.L.CallByParam(lua.P{
			Fn:      fn,
			NRet:    0,
			Protect: true,
		}, arg); err != nil {
			r.log.Error("lua: handler error", "event", key, "handler_index", i, "err", err)
			// Continue calling remaining handlers.
		}
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
	return t
}
