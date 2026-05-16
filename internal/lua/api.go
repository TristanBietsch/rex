package lua

import (
	"log/slog"

	lua "github.com/yuin/gopher-lua"
)

// registerAPI installs the "rex" global table into r.L.
// Must be called once during New(), before any user script is loaded.
func registerAPI(r *Runtime) {
	t := r.L.NewTable()

	r.L.SetField(t, "send", r.L.NewFunction(r.luaSend))
	r.L.SetField(t, "list", r.L.NewFunction(r.luaList))
	r.L.SetField(t, "log", r.L.NewFunction(r.luaLog))
	r.L.SetField(t, "on", r.L.NewFunction(r.luaOn))

	r.L.SetGlobal("rex", t)
}

// luaSend implements rex.send(session_id, text).
//
// Calls the injected Sender callback. Any error is surfaced as a Lua error.
func (r *Runtime) luaSend(L *lua.LState) int {
	sessionID := L.CheckString(1)
	text := L.CheckString(2)

	r.log.Debug("lua: rex.send called", "session_id", sessionID)

	if err := r.opts.Sender(sessionID, text); err != nil {
		L.RaiseError("rex.send: %v", err)
		return 0
	}
	return 0
}

// luaList implements rex.list() -> table-of-tables.
//
// Returns a Lua array table where each element is a table with the fields:
// id, short_id, slug, title, state, tool_id, model_id, cwd.
func (r *Runtime) luaList(L *lua.LState) int {
	sessions := r.opts.Lister()

	result := L.NewTable()
	for i, s := range sessions {
		t := summaryToTable(L, s)
		L.RawSetInt(result, i+1, t)
	}

	r.log.Debug("lua: rex.list called", "count", len(sessions))
	L.Push(result)
	return 1
}

// luaLog implements rex.log(level, msg).
//
// level must be one of "info", "warn", "error"; anything else defaults to "info".
func (r *Runtime) luaLog(L *lua.LState) int {
	level := L.CheckString(1)
	msg := L.CheckString(2)

	switch level {
	case "warn":
		r.log.Warn(msg, "source", "lua")
	case "error":
		r.log.Error(msg, "source", "lua")
	default:
		r.log.Info(msg, "source", "lua")
	}
	return 0
}

// validEvents is the set of event names accepted by rex.on(). Keys use the
// lowercase Lua-friendly form; OnEvent maps protocol constants to these same
// keys before dispatching.
var validEvents = map[string]bool{
	"session_added":   true,
	"session_updated": true,
	"session_removed": true,
}

// luaOn implements rex.on(event_name, fn).
//
// Supported event names: "session_added", "session_updated", "session_removed".
// Multiple handlers may be registered for the same event; they are called in
// registration order.
func (r *Runtime) luaOn(L *lua.LState) int {
	eventName := L.CheckString(1)
	fn, ok := L.Get(2).(*lua.LFunction)
	if !ok {
		L.ArgError(2, "function expected")
		return 0
	}

	if !validEvents[eventName] {
		L.ArgError(1, "unknown event name: "+eventName)
		return 0
	}

	r.handlers[eventName] = append(r.handlers[eventName], fn)
	r.log.Debug("lua: handler registered", "event", eventName)
	return 0
}

// slogLevelFromString maps a string to a slog.Level (unexported helper used in tests).
func slogLevelFromString(s string) slog.Level {
	switch s {
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
