package boot

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/lua"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// startLua is StartLuaHost's start after the config path and callbacks are known.
func startLua(cfgPath string, store *state.Store, opts lua.Options) (*lua.Runtime, func()) {
	rt, err := lua.New(opts)
	if err != nil {
		slog.Error("daemon: lua init failed", "err", err)
		return nil, nil
	}

	if err := rt.LoadFile(cfgPath); err != nil {
		slog.Error("daemon: lua load failed; runtime still active for future reloads", "path", cfgPath, "err", err)
	}

	return rt, subscribeLua(rt, store)
}

// subscribeLua feeds store events to rt through a dispatcher; the returned
// func stops it.
func subscribeLua(rt *lua.Runtime, store *state.Store) func() {
	d := lua.NewDispatcher(rt, lua.DispatcherOptions{})
	unsubscribe := store.Subscribe(func(e state.Event) {
		if eventType, data, ok := luaEvent(store, e); ok {
			d.Enqueue(eventType, e.SessionID, data)
		}
	})
	return func() {
		unsubscribe()
		d.Close()
	}
}

// luaEvent maps a store event to the event type and payload Lua handlers
// receive. It runs inside Store.broadcast, so it only reads the store and
// never calls into Lua. session_updated carries the session's whole current
// summary, which is what lets the dispatcher keep only the latest one.
func luaEvent(store *state.Store, e state.Event) (eventType string, data any, ok bool) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary == nil {
			return "", nil, false
		}
		return protocol.EventSessionAdded, *e.Summary, true
	case state.EventUpdated:
		sess, found := store.Get(e.SessionID)
		if !found {
			return "", nil, false
		}
		return protocol.EventSessionUpdated, sess.Summary(), true
	case state.EventRemoved:
		return protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: e.SessionID}, true
	default:
		return "", nil, false
	}
}

// luaConfigPathFrom returns the resolved path to the user's init.lua from
// the lua_config_path setting in st, or "" if not configured or when a "~/"
// path cannot be resolved (no home directory).
func luaConfigPathFrom(st *settings.Store) string {
	raw := st.String("lua_config_path")
	if raw == "" {
		return ""
	}
	p, err := paths.ExpandHome(raw)
	if err != nil {
		slog.Warn("daemon: lua disabled; cannot resolve lua_config_path", "path", raw, "err", err)
		return ""
	}
	return p
}

// LuaHost owns the daemon's init.lua runtime so a reload can rebuild it
// (R20). The path is lua_config_path as read at daemon start (that setting
// applies after a restart).
type LuaHost struct {
	mu    sync.Mutex
	path  string
	store *state.Store
	opts  lua.Options
	rt    *lua.Runtime
	stop  func()
}

// StartLuaHost is StartLuaRuntime behind a LuaHost. It never fails: with no
// path the host holds nothing and Reload does nothing.
func StartLuaHost(srv *server.Server, store *state.Store, st *settings.Store) *LuaHost {
	h := &LuaHost{path: luaConfigPathFrom(st), store: store, opts: lua.Options{
		Sender: func(sessionID, text string) error {
			return srv.SendRaw(sessionID, []byte(text), lua.DefaultSendTimeout)
		},
		Lister: func() []protocol.SessionSummary { return store.Snapshot() },
	}}
	if h.path == "" {
		slog.Info("daemon: lua disabled (no config path)")
		return h
	}
	h.rt, h.stop = startLua(h.path, store, h.opts)
	return h
}

// Path is init.lua's path, "" when Lua is off.
func (h *LuaHost) Path() string { return h.path }

// Reload rebuilds the runtime from init.lua. A file that fails to load
// keeps the previous runtime and its handlers (the error names
// file:line); a missing file leaves no handlers.
func (h *LuaHost) Reload() error {
	if h == nil || h.path == "" {
		return nil
	}
	rt, err := lua.New(h.opts)
	if err != nil {
		return fmt.Errorf("init.lua: %w", err)
	}
	if err := rt.LoadFile(h.path); err != nil {
		rt.Close()
		return luaFileError(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// The old dispatcher stops before the new one starts: an event is
	// never handled twice (a handler may send input to a session).
	if h.stop != nil {
		h.stop()
	}
	if h.rt != nil {
		h.rt.Close()
	}
	h.rt = rt
	h.stop = subscribeLua(rt, h.store)
	slog.Info("daemon: init.lua reloaded", "path", h.path)
	return nil
}

// Close stops the runtime.
func (h *LuaHost) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stop != nil {
		h.stop()
		h.stop = nil
	}
	if h.rt != nil {
		h.rt.Close()
		h.rt = nil
	}
}

// luaSyntaxPos matches gopher-lua's parse error position,
// "path line:N(column:M) near ...", so it can read path:N: near ....
var luaSyntaxPos = regexp.MustCompile(` line:(\d+)\(column:\d+\) `)

// luaFileError strips the runtime's `lua: LoadFile "path":` prefix and the
// stack traceback so the message reads file:line: message on one line.
func luaFileError(err error) error {
	msg := err.Error()
	if i := strings.Index(msg, "\": "); strings.HasPrefix(msg, "lua: LoadFile \"") && i > 0 {
		msg = msg[i+3:]
	}
	if i := strings.Index(msg, "\nstack traceback:"); i >= 0 {
		msg = msg[:i]
	}
	if loc := luaSyntaxPos.FindStringSubmatchIndex(msg); loc != nil {
		msg = msg[:loc[0]] + ":" + msg[loc[2]:loc[3]] + ": " + msg[loc[1]:]
	}
	return errors.New(protocol.FlattenDisplayText(strings.TrimSpace(msg)))
}
