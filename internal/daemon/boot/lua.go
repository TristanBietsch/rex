package boot

import (
	"log/slog"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/lua"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// StartLuaRuntime initializes the Lua scripting runtime and subscribes it to
// store events. Returns (nil, nil) if Lua is disabled or fails to init —
// daemon startup must not depend on user scripts.
//
// Running init.lua is bounded by lua.DefaultLoadTimeout. Handlers never run
// on the store's broadcast path: events go through a lua.Dispatcher (bounded
// queue, one goroutine, each call bounded by lua.DefaultHandlerTimeout). The
// returned func unsubscribes from the store, then stops the dispatcher and
// waits for it, interrupting a handler in flight. It may run before or after
// Runtime.Close.
func StartLuaRuntime(srv *server.Server, store *state.Store, st *settings.Store) (*lua.Runtime, func()) {
	cfgPath := luaConfigPathFrom(st)
	if cfgPath == "" {
		slog.Info("daemon: lua disabled (no config path)")
		return nil, nil
	}
	return startLua(cfgPath, store, lua.Options{
		Sender: func(sessionID, text string) error {
			// Through the session's input lock, so Lua input never lands in
			// the middle of a loop's send. It is not the user's activity: a
			// loop does not hold back for it.
			return srv.SendRaw(sessionID, []byte(text), lua.DefaultSendTimeout)
		},
		Lister: func() []protocol.SessionSummary {
			return store.Snapshot()
		},
	})
}

// startLua is StartLuaRuntime after the config path and callbacks are known.
func startLua(cfgPath string, store *state.Store, opts lua.Options) (*lua.Runtime, func()) {
	rt, err := lua.New(opts)
	if err != nil {
		slog.Error("daemon: lua init failed", "err", err)
		return nil, nil
	}

	if err := rt.LoadFile(cfgPath); err != nil {
		slog.Error("daemon: lua load failed; runtime still active for future reloads", "path", cfgPath, "err", err)
	}

	d := lua.NewDispatcher(rt, lua.DispatcherOptions{})
	unsubscribe := store.Subscribe(func(e state.Event) {
		if eventType, data, ok := luaEvent(store, e); ok {
			d.Enqueue(eventType, e.SessionID, data)
		}
	})

	return rt, func() {
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
