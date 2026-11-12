// Package luacfg evaluates ~/.config/rex/config.lua, the optional file that
// sets settings and themes in Lua (the lua_config feature).
//
// The file runs in a fresh, restricted gopher-lua state with exactly three
// entry points: rex.set(key, value), rex.theme(name, table) and
// rex.platform (runtime.GOOS). There is no os, io, package, debug or
// coroutine library, no print, no load/dofile/pcall, and require reads only
// from the module directory (~/.config/rex/lua). The result depends only on
// the files and the platform, so the daemon, the TUI and the CLI always
// agree, and it is memoised per process by file stamp (Load).
//
// The restrictions are for liveness and for keeping Lua text off the
// terminal, not a security boundary: config.lua is trusted user code, owned
// by the user and not writable by others (package privfile). Evaluation is
// bounded by a wall-clock timeout, size caps and a heap-growth watchdog; a
// script stuck inside a Go built-in is abandoned at the deadline.
package luacfg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/privfile"
)

// Limits on one evaluation.
const (
	// Timeout is the wall-clock budget of one evaluation, in every process.
	Timeout = 250 * time.Millisecond
	// MaxScript is the largest config.lua or module file, in bytes.
	MaxScript = 256 << 10
	// MaxTotal is the most source the script and its modules may add up to.
	MaxTotal = 1 << 20
	// MaxValue is the longest string value rex.set accepts, in bytes.
	MaxValue = 4 << 10
	// MaxModules is the most modules one evaluation may require.
	MaxModules = 64
	// MaxWarnings is how many warnings are kept before "N more".
	MaxWarnings = 20
	// MaxHeapGrowth stops a script whose evaluation grows the process heap
	// by this much. The measure is process-wide, so it is approximate.
	MaxHeapGrowth = 64 << 20

	maxPatternSubject = 256 << 10
	maxPattern        = 1 << 10
	maxReplacement    = 1 << 10
	maxTableDepth     = 3
	maxTableKeys      = 64
	maxErrRunes       = 200
)

// Opts says what to evaluate.
type Opts struct {
	// File is the config.lua path (paths.LuaConfigFile).
	File string
	// ModuleDir is the only directory require reads from (paths.LuaModuleDir).
	// Empty: require is refused.
	ModuleDir string
	// Deny lists setting ids rex.set refuses, on top of the ones nothing may
	// set (lua_config, read-only rows). R06 uses it for workspace files.
	Deny []string
	// Chunk names the file in positions ("<Chunk>:12") and errors. Empty
	// is "config.lua".
	Chunk string
	// Read, when set, returns the script instead of a read of File through
	// package privfile; File still keys the memo and the Stamp. An error
	// wrapping fs.ErrNotExist means there is no file. A workspace's
	// .rex/config.lua is read this way, with the workspace package's
	// stricter checks (no symlink at all).
	Read func() ([]byte, error)
}

// chunk is o.Chunk or "config.lua".
func (o Opts) chunk() string {
	if o.Chunk != "" {
		return o.Chunk
	}
	return "config.lua"
}

// Result is what one evaluation produced. Maps are shared with the memo:
// treat them as read-only.
type Result struct {
	// Exists is true when config.lua exists (even if it failed).
	Exists bool
	// Values maps setting id to the validated value rex.set stored.
	Values map[string]any
	// SetAt maps setting id to "config.lua:12" or "lua/ui.lua:3".
	SetAt map[string]string
	// Themes holds the themes rex.theme defined, by name.
	Themes map[string]theme.Palette
	// ThemeIssues are the problems and notes about Lua themes (File is the
	// "config.lua:12" position).
	ThemeIssues []theme.Issue
	// FailClosed lists the trust settings (settings.TrustSettingIDs) whose
	// last rex.set was refused as invalid: the layer puts them back to
	// their defaults, whatever config.yaml says, since the user meant to
	// change them.
	FailClosed []string
	// Warnings are refused rex.set calls and the like, at most MaxWarnings
	// plus a "N more" line. They are safe to print.
	Warnings []string
	// Err is why the file did not evaluate: refused, a syntax or runtime
	// error, a timeout or a limit. The text is safe to print. When Err is
	// set Values, SetAt and Themes are empty, or the last good ones when
	// LastGood is true.
	Err error
	// LastGood is true when Err is set and the other fields hold the
	// previous good evaluation of this process.
	LastGood bool
}

// touches counts filesystem touches (stat or read) of config.lua and lua/.
var touches atomic.Int64

// Touches returns how many times this process stat'ed or read config.lua or
// its modules. Tests use it to prove a disabled feature is inert.
func Touches() int64 { return touches.Load() }

func touch() { touches.Add(1) }

// Eval evaluates o.File now, without the memo. A missing file is an empty
// Result (Exists false).
func Eval(ctx context.Context, o Opts) Result {
	touch()
	if o.Read != nil {
		return evalRead(ctx, o)
	}
	dir, name := filepath.Dir(o.File), filepath.Base(o.File)
	fi, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{}
		}
		return fail(fmt.Errorf("%s: %w", name, err))
	}
	data, err := privfile.Read(dir, name, MaxScript)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{}
		}
		return fail(fmt.Errorf("%s: %w", name, err))
	}
	if err := privfile.CheckDir(fi, dir); err != nil {
		return fail(fmt.Errorf("config directory: %w", err))
	}
	return evalSource(ctx, o, data)
}

// evalRead is Eval through o.Read.
func evalRead(ctx context.Context, o Opts) Result {
	data, err := o.Read()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Result{}
	case err != nil:
		return fail(fmt.Errorf("%s: %w", o.chunk(), err))
	case len(data) > MaxScript:
		return fail(fmt.Errorf("%s: file is larger than %d KiB; refused", o.chunk(), MaxScript>>10))
	}
	return evalSource(ctx, o, data)
}

func fail(err error) Result {
	return Result{Exists: true, Err: errors.New(flatten(err.Error(), maxErrRunes))}
}

func evalSource(parent context.Context, o Opts, src []byte) Result {
	ctx, cancel := context.WithTimeout(parent, Timeout)
	defer cancel()
	ev := newEvaluator(o, len(src))
	done := make(chan Result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- Result{Exists: true, Err: fmt.Errorf("%s: internal error: %v", flatten(o.chunk(), maxErrRunes), flatten(fmt.Sprint(r), 80))}
			}
		}()
		done <- ev.run(ctx, src)
	}()
	go ev.watchHeap(ctx, cancel)
	select {
	case r := <-done:
		if r.Err == nil || ctx.Err() == nil {
			return r
		}
		// The script noticed the cancel first: report why it was stopped,
		// not the Lua state's own "context canceled" text.
		r.Values, r.SetAt, r.Themes, r.ThemeIssues = nil, nil, nil, nil
		r.Err = stopReason(ev, ctx)
		return r
	case <-ctx.Done():
		// A script stuck in a Go built-in cannot be interrupted; it is
		// abandoned here and stops at its next VM instruction check.
		return Result{Exists: true, Err: stopReason(ev, ctx)}
	}
}

// stopReason says why ctx stopped the evaluation.
func stopReason(ev *evaluator, ctx context.Context) error {
	name := flatten(ev.opts.chunk(), maxErrRunes)
	switch {
	case ev.heap.Load():
		return fmt.Errorf("%s: stopped: the script grew memory by more than %d MiB", name, MaxHeapGrowth>>20)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s: timed out after %s", name, Timeout)
	}
	return fmt.Errorf("%s: stopped: %w", name, ctx.Err())
}

// heapBytes reads the live heap size without stopping the world.
func heapBytes() uint64 {
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return s[0].Value.Uint64()
}

// watchHeap cancels the evaluation when the process heap grows by
// MaxHeapGrowth. It runs until ctx is done.
func (ev *evaluator) watchHeap(ctx context.Context, cancel context.CancelFunc) {
	base := heapBytes()
	t := time.NewTicker(time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if cur := heapBytes(); cur > base && cur-base > MaxHeapGrowth {
				ev.heap.Store(true)
				cancel()
				return
			}
		}
	}
}

// run executes the script on the evaluator's own state.
func (ev *evaluator) run(ctx context.Context, src []byte) Result {
	L := lua.NewState(lua.Options{
		SkipOpenLibs:     true,
		CallStackSize:    200,
		RegistrySize:     8 * 1024,
		RegistryMaxSize:  64 * 1024,
		RegistryGrowStep: 32,
	})
	defer L.Close()
	L.SetContext(ctx)
	ev.sandbox(L)
	fn, err := L.Load(bytes.NewReader(src), ev.opts.chunk())
	if err == nil {
		L.Push(fn)
		err = L.PCall(0, lua.MultRet, nil)
	}
	if err != nil {
		return Result{Exists: true, Err: errors.New(flatten(luaMessage(err), maxErrRunes)), Warnings: ev.warnings()}
	}
	return Result{
		Exists: true, Values: ev.values, SetAt: ev.setAt,
		Themes: ev.themes, ThemeIssues: ev.issues, Warnings: ev.warnings(),
		FailClosed: ev.failClosed(),
	}
}

// luaMessage is the error value of a Lua failure without Go or Lua stack
// traces.
func luaMessage(err error) string {
	var ae *lua.ApiError
	if errors.As(err, &ae) && ae.Object != nil {
		return ae.Object.String()
	}
	return err.Error()
}

// flatten makes s safe to print: it keeps the first line, replaces every
// control, bidirectional or non-printing rune with '?', and cuts it to max
// runes.
func flatten(s string, max int) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n++; n > max {
			b.WriteString("...")
			break
		}
		if unsafeRune(r) {
			r = '?'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func platform() string { return runtime.GOOS }
