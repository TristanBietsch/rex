package boot

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/lua"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// luaQueueSize bounds the store events waiting for Lua handlers.
const luaQueueSize = 256

// StartLuaRuntime initializes the Lua scripting runtime and feeds it store
// events. It returns the func that stops it (a no-op when Lua is disabled or
// fails to init — daemon startup must not depend on user scripts).
func StartLuaRuntime(srv *server.Server, store *state.Store) (stop func()) {
	cfgPath := luaConfigPath()
	if cfgPath == "" {
		slog.Info("daemon: lua disabled (no config path)")
		return func() {}
	}

	rt, err := lua.New(lua.Options{
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
	if err != nil {
		slog.Error("daemon: lua init failed", "err", err)
		return func() {}
	}

	if err := rt.LoadFile(cfgPath); err != nil {
		slog.Error("daemon: lua load failed; handlers registered before the error stay active (restart the daemon after fixing the script)",
			"path", cfgPath, "err", err)
	}

	return dispatchLua(rt, store, luaQueueSize)
}

// luaEvent is one store event, converted for Lua at broadcast time.
type luaEvent struct {
	typ  string
	data any
}

// dispatchLua runs rt's handlers for store events on a goroutine of its own.
// The store broadcasts synchronously from session supervisors and client
// handlers, so a slow or stuck script must never run inside the broadcast:
// events go through a queue of size n, and while it is full they are dropped
// and logged. The returned stop unsubscribes, waits for the dispatcher (an
// in-flight handler ends within lua.Options.CallTimeout) and closes rt.
func dispatchLua(rt *lua.Runtime, store *state.Store, n int) (stop func()) {
	events := make(chan luaEvent, n)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-quit:
				return
			case ev := <-events:
				_ = rt.OnEvent(ev.typ, ev.data)
			}
		}
	}()

	var dropped atomic.Int64
	unsubscribe := store.Subscribe(func(e state.Event) {
		ev, ok := toLuaEvent(store, e)
		if !ok {
			return
		}
		select {
		case events <- ev:
			if d := dropped.Swap(0); d > 0 {
				slog.Warn("daemon: lua events dropped while the queue was full", "dropped", d)
			}
		default:
			if dropped.Add(1) == 1 {
				slog.Warn("daemon: lua event queue full; dropping events (slow init.lua handler?)", "queue", n)
			}
		}
	})

	return func() {
		// Once Subscribe's cancel returns no broadcast is still inside the
		// callback, so nothing sends after quit.
		unsubscribe()
		close(quit)
		<-done
		rt.Close()
	}
}

// toLuaEvent converts a store event for the Lua handlers.
func toLuaEvent(store *state.Store, e state.Event) (luaEvent, bool) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil {
			return luaEvent{protocol.EventSessionAdded, *e.Summary}, true
		}
	case state.EventUpdated:
		if sess, ok := store.Get(e.SessionID); ok {
			return luaEvent{protocol.EventSessionUpdated, sess.Summary()}, true
		}
	case state.EventRemoved:
		return luaEvent{protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: e.SessionID}}, true
	}
	return luaEvent{}, false
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
