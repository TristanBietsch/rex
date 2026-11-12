package luacfg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Limits on the stamp walk of the module directory.
const (
	maxStampFiles = 256
	maxStampDepth = 4
)

// Stamp summarizes config.lua and every .lua file below ModuleDir (names,
// sizes, modification times) as a string that changes when any of them is
// added, removed or edited. A missing config.lua has the empty stamp: there
// is nothing to evaluate, whatever lua/ holds. It stats paths, not inodes,
// so an editor that saves by rename is seen.
func Stamp(o Opts) string {
	touch()
	fi, err := os.Stat(o.File)
	switch {
	case os.IsNotExist(err):
		return ""
	case err != nil:
		return "?:" + err.Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d:%d;", fi.Size(), fi.ModTime().UnixNano())
	if o.ModuleDir != "" {
		n := 0
		stampDir(&b, o.ModuleDir, 0, &n)
	}
	return b.String()
}

func stampDir(b *strings.Builder, dir string, depth int, n *int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries { // sorted by name
		if *n >= maxStampFiles {
			return
		}
		p := filepath.Join(dir, e.Name())
		fi, err := os.Stat(p) // follows a link
		if err != nil {
			fmt.Fprintf(b, "%s:?;", e.Name())
			*n++
			continue
		}
		switch {
		case fi.IsDir():
			if depth < maxStampDepth {
				fmt.Fprintf(b, "%s/", e.Name())
				stampDir(b, p, depth+1, n)
			}
		case strings.HasSuffix(e.Name(), ".lua"):
			fmt.Fprintf(b, "%s:%d:%d;", e.Name(), fi.Size(), fi.ModTime().UnixNano())
			*n++
		}
	}
}

type memoEntry struct {
	stamp string
	res   Result
	good  *Result // the last evaluation without an error
}

var (
	memoMu sync.Mutex
	memo   = map[string]*memoEntry{}
)

// ResetMemo forgets every memoised evaluation (tests).
func ResetMemo() {
	memoMu.Lock()
	defer memoMu.Unlock()
	clear(memo)
}

func memoKey(o Opts) string {
	return o.File + "\x00" + o.ModuleDir + "\x00" + strings.Join(o.Deny, "\x00")
}

// Load is Eval memoised per process by Stamp: the files are evaluated once
// until they change. When an edit breaks the file, the result carries Err and
// the previous good Values (LastGood), so a typo never reverts the user's
// settings; a process that never saw a good evaluation has none.
func Load(ctx context.Context, o Opts) Result {
	stamp := Stamp(o)
	key := memoKey(o)
	memoMu.Lock()
	defer memoMu.Unlock()
	e := memo[key]
	if e != nil && e.stamp == stamp {
		return e.res
	}
	if e == nil {
		if len(memo) >= 16 {
			clear(memo)
		}
		e = &memoEntry{}
		memo[key] = e
	}
	var res Result
	if stamp != "" {
		res = Eval(ctx, o)
	}
	switch {
	case res.Err == nil:
		e.good = &res
	case e.good != nil:
		res.Values, res.SetAt, res.Themes, res.ThemeIssues = e.good.Values, e.good.SetAt, e.good.Themes, e.good.ThemeIssues
		res.FailClosed = e.good.FailClosed
		res.LastGood = true
	}
	e.stamp, e.res = stamp, res
	return res
}
