package boot

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/lua"
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
func StartLuaRuntime(srv *server.Server, store *state.Store) (*lua.Runtime, func()) {
	cfgPath := luaConfigPath()
	if cfgPath == "" {
		slog.Info("daemon: lua disabled (no config path)")
		return nil, nil
	}
	return startLua(cfgPath, store, lua.Options{
		Sender: func(sessionID, text string) error {
			ch := srv.InputChannel(sessionID)
			if ch == nil {
				return fmt.Errorf("session %q has no input channel", sessionID)
			}
			payload := []byte(text)
			select {
			case ch <- payload:
				return nil
			case <-time.After(lua.DefaultSendTimeout):
				return errors.New("send timed out")
			}
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

// luaConfigPath returns the resolved path to the user's init.lua, or "" if not configured.
func luaConfigPath() string {
	st := settings.NewStore()
	if err := st.Load(settings.DefaultPath()); err != nil {
		slog.Warn("daemon: settings load failed for lua path", "err", err)
	}
	raw := st.String("lua_config_path")
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "~/") {
		home, _ := os.UserHomeDir()
		raw = filepath.Join(home, raw[2:])
	}
	return raw
}
