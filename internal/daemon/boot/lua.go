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
func StartLuaRuntime(srv *server.Server, store *state.Store) (*lua.Runtime, func()) {
	cfgPath := luaConfigPath()
	if cfgPath == "" {
		slog.Info("daemon: lua disabled (no config path)")
		return nil, nil
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
		return nil, nil
	}

	if err := rt.LoadFile(cfgPath); err != nil {
		slog.Error("daemon: lua load failed; runtime still active for future reloads", "path", cfgPath, "err", err)
	}

	cancel := store.Subscribe(func(e state.Event) {
		switch e.Kind {
		case state.EventAdded:
			if e.Summary != nil {
				_ = rt.OnEvent(protocol.EventSessionAdded, *e.Summary)
			}
		case state.EventUpdated:
			sess, ok := store.Get(e.SessionID)
			if !ok {
				return
			}
			_ = rt.OnEvent(protocol.EventSessionUpdated, sess.Summary())
		case state.EventRemoved:
			_ = rt.OnEvent(protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: e.SessionID})
		}
	})

	return rt, cancel
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
