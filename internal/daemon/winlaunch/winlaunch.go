// Package winlaunch keeps an agent's prompt away from cmd.exe on Windows.
//
// CreateProcess runs a .cmd or .bat file through cmd.exe, which parses the
// whole command line again: "&", "|", "<", ">", "^", "%VAR%", "!VAR!",
// quotes and line breaks in an argument become cmd syntax. npm installs
// agent CLIs (claude.cmd, qwen.cmd, gemini.cmd) as such shims, and the task
// text rides on argv, so a prompt could run commands. Windows also caps a
// command line at 32,767 UTF-16 units, below the daemon's 96 KiB prompt cap.
//
// Plan decides how a launch goes:
//
//  1. a shim npm generated is resolved to what it runs (node.exe and the
//     script, or the package's own .exe) and that is launched directly, so
//     cmd.exe never sees the arguments;
//  2. a shim that cannot be resolved is launched only with arguments that
//     have no cmd.exe meaning (SafeArg), and the prompt goes through the PTY
//     paste path instead of argv;
//  3. anything else is refused with an error that names the shim.
//
// A command line over MaxCmdLine moves the prompt to the paste path too, or
// is refused when there is nothing to move.
//
// The decision is pure over an Env, so it is tested on every platform; the
// daemon calls it only on Windows (Active).
package winlaunch

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"
)

// MaxCmdLine is the longest command line Plan lets through, in UTF-16 units
// (CmdLineBound). Windows allows 32,767 including the terminating NUL; the
// rest is headroom for what CreateProcess and the C runtime add.
const MaxCmdLine = 32000

// MaxShellLine is the cap when cmd.exe parses the command line (an
// unresolved shim, or cmd.exe as the command): cmd.exe takes 8,191.
const MaxShellLine = 8000

// maxShimBytes bounds a shim read: npm's are a few hundred bytes.
const maxShimBytes = 64 << 10

// ErrRefused is wrapped by every refusal.
var ErrRefused = errors.New("refused to start on Windows")

// Env is what Plan reads from the system.
type Env struct {
	// LookPath resolves a command name to a file, as exec.LookPath does.
	LookPath func(name string) (string, error)
	// ReadFile reads a shim; at most maxShimBytes are used.
	ReadFile func(path string) ([]byte, error)
	// IsFile reports whether path is an existing regular file.
	IsFile func(path string) bool
}

// Launch is a planned launch.
type Launch struct {
	// Argv is the command to start: Argv[0] is an absolute path that is not
	// a batch file, or a batch file whose arguments all pass SafeArg.
	Argv []string
	// Paste is the prompt to paste into the PTY ("" for none). It is set when
	// the prompt left argv (an unresolved shim, or a command line too long).
	Paste string
	// Via says what was done, for the daemon log: "direct", "shim:node",
	// "shim:exe", "shim:safe-args", "cmd.exe".
	Via string
	// PromptMoved is true when the prompt left argv for the paste path, and
	// Reason says why ("cmd.exe", "length").
	PromptMoved bool
	Reason      string
}

// Request is one launch to plan.
type Request struct {
	// Base is the argv without the prompt (buildArgv).
	Base []string
	// PromptArgs are the argv elements that carry the prompt, appended to
	// Base (nil when the prompt is pasted or there is none).
	PromptArgs []string
	// Prompt is the prompt text; with PromptArgs nil and Prompt set, it is
	// already bound for the paste path.
	Prompt string
}

// IsBatch reports whether path names a file CreateProcess hands to cmd.exe.
// Windows drops trailing dots and spaces from a file name ("x.cmd. " opens
// x.cmd) and reads "x.cmd:s" or "x.cmd::$DATA" as a stream of x.cmd, so the
// name is normalized the same way first (winName).
func IsBatch(path string) bool {
	switch strings.ToLower(filepath.Ext(winName(path))) {
	case ".cmd", ".bat":
		return true
	}
	return false
}

// IsCmdShell reports whether path is cmd.exe itself (a registry command
// such as ["cmd", "/c", "tool"]).
func IsCmdShell(path string) bool {
	b := winName(path)
	return strings.EqualFold(b, "cmd.exe") || strings.EqualFold(b, "cmd")
}

// SafeArg reports whether s has no meaning to cmd.exe once Go has quoted it:
// printable ASCII letters and digits, space and - _ . , : / \ = + @ ~ #.
// Everything else is out: the operators & | < > ^, % and ! (variable
// expansion), quotes and parentheses, every control character (a line break
// ends the command), and all non-ASCII text (best-fit code page mapping can
// turn a fullwidth quote into a real one). The empty string is out too
// (Go quotes it as "").
func SafeArg(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte(" -_.,:/\\=+@~#", c) >= 0:
		default:
			return false
		}
	}
	// A trailing backslash would be doubled inside quotes; harmless, but a
	// "\" right before Go's closing quote is the classic escape confusion.
	return !strings.ContainsRune(s, ' ') || !strings.HasSuffix(s, `\`)
}

// CmdLineBound is an upper bound on the UTF-16 length of the command line
// Go composes for args (windows.ComposeCommandLine): each argument at most
// doubles its backslashes, escapes each quote and gains two quotes and a
// separating space.
func CmdLineBound(args []string) int {
	n := 0
	for _, a := range args {
		n += len(utf16.Encode([]rune(a))) + strings.Count(a, `\`) + strings.Count(a, `"`) + 3
	}
	return n
}

// npm cmd-shim lines name the target relative to the shim's folder:
//
//	"%dp0%\node_modules\pkg\cli.js" %*          (npm 7+)
//	"%~dp0\node_modules\pkg\cli.js" %*          (npm 6)
//	"%dp0%\node_modules\pkg\bin\tool.exe"  %*   (a native binary)
var shimTarget = regexp.MustCompile(`"%(?:dp0%|~dp0)\\?([^"%]+)"\s+%\*`)

// ParseShim returns the target an npm cmd shim runs, relative to the shim's
// folder ("node_modules\pkg\cli.js"), and whether it runs it through node.
// ok is false when text is not a shim npm generated or names more than one
// distinct target.
func ParseShim(text string) (target string, viaNode bool, ok bool) {
	ms := shimTarget.FindAllStringSubmatch(text, -1)
	if len(ms) == 0 {
		return "", false, false
	}
	target = ms[0][1]
	for _, m := range ms[1:] {
		if m[1] != target {
			return "", false, false
		}
	}
	target = strings.TrimLeft(target, `\/`)
	if target == "" || strings.ContainsAny(target, "\r\n:") {
		return "", false, false
	}
	switch strings.ToLower(filepath.Ext(winBase(target))) {
	case ".js", ".cjs", ".mjs":
		viaNode = true
	case ".exe":
	default:
		return "", false, false
	}
	return target, viaNode, true
}

// Plan decides how to launch r on Windows (see the package doc).
func Plan(r Request, env Env) (Launch, error) {
	if len(r.Base) == 0 {
		return Launch{}, errors.New("winlaunch: empty command")
	}
	exe, err := env.LookPath(r.Base[0])
	if err != nil {
		return Launch{}, err
	}
	if !winAbs(exe) {
		// conpty would resolve a relative program against the session
		// folder, and a shim's target against the daemon's.
		return Launch{}, fmt.Errorf("%w: %s does not resolve to an absolute path", ErrRefused, exe)
	}
	prefix := []string{exe}
	via := "direct"
	resolved := true
	shell := IsCmdShell(exe)
	if IsBatch(exe) {
		prefix, via, resolved = resolveShim(exe, env)
	} else if shell {
		via = "cmd.exe"
	}
	l := Launch{Via: via}
	args := r.Base[1:]
	prompt := r.PromptArgs
	limit := MaxCmdLine
	if !resolved || shell {
		// cmd.exe will parse this command line: the prompt never goes on
		// it, and every other argument (rex adds paths and JSON after the
		// registry command) must mean nothing to cmd.exe.
		limit = MaxShellLine
		if len(prompt) > 0 {
			prompt, l.Paste, l.PromptMoved, l.Reason = nil, r.Prompt, true, "cmd.exe"
		}
		if bad := firstUnsafe(append([]string{exe}, args...)); bad != "" {
			return Launch{}, fmt.Errorf("%w: %s runs through cmd.exe, and the argument %q holds characters cmd.exe "+
				"would interpret (only ASCII letters, digits, space and - _ . , : / \\ = + @ ~ # are let through); "+
				"install the tool's native .exe or set the tool's command to the program itself: %w",
				ErrRefused, exe, bad, errUnsafeArgs)
		}
	}
	l.Argv = append(append(append([]string{}, prefix...), args...), prompt...)
	if CmdLineBound(l.Argv) > limit {
		if len(prompt) == 0 {
			return Launch{}, fmt.Errorf("%w: the command line for %s is over %d characters", ErrRefused, exe, limit)
		}
		l.Argv = l.Argv[:len(l.Argv)-len(prompt)]
		l.Paste, l.PromptMoved, l.Reason = r.Prompt, true, "length"
		if CmdLineBound(l.Argv) > limit {
			return Launch{}, fmt.Errorf("%w: the command line for %s is over %d characters without the prompt", ErrRefused, exe, limit)
		}
	}
	if !l.PromptMoved && len(r.PromptArgs) == 0 {
		l.Paste = r.Prompt
	}
	return l, nil
}

var errUnsafeArgs = errors.New("an argument holds characters cmd.exe would interpret")

// firstUnsafe returns the first element of args SafeArg refuses ("" for
// none; an empty argument is reported as "").
func firstUnsafe(args []string) string {
	for _, a := range args {
		if !SafeArg(a) {
			if a == "" {
				return `""`
			}
			return a
		}
	}
	return ""
}

// winAbs reports whether p is a drive-rooted ("C:\x") or UNC ("\\h\s")
// path.
func winAbs(p string) bool {
	p = winSlash(p)
	if strings.HasPrefix(p, `\\`) {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && p[2] == '\\' &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

// resolveShim turns an npm shim into the program it runs. ok is false when
// the shim is not one npm generated, its target escapes the shim's folder or
// is missing, or no node.exe is found.
func resolveShim(shim string, env Env) ([]string, string, bool) {
	unresolved := []string{shim}
	b, err := env.ReadFile(shim)
	if err != nil || len(b) > maxShimBytes {
		return unresolved, "shim:safe-args", false
	}
	rel, viaNode, ok := ParseShim(string(b))
	if !ok {
		return unresolved, "shim:safe-args", false
	}
	dir := winDir(shim)
	target, ok := winJoinInside(dir, rel)
	if !ok || !env.IsFile(target) {
		return unresolved, "shim:safe-args", false
	}
	if !viaNode {
		return []string{target}, "shim:exe", true
	}
	// npm's shim prefers a node.exe next to itself, then node on PATH.
	node := winDir(shim) + `\node.exe`
	if !env.IsFile(node) {
		p, err := env.LookPath("node")
		if err != nil || IsBatch(p) {
			return unresolved, "shim:safe-args", false
		}
		node = p
	}
	return []string{node, target}, "shim:node", true
}

// The helpers below treat paths as Windows paths whatever the host, so Plan
// behaves the same in tests on every platform.

// winName is the file name Windows opens for p: the base, cut at the first
// ":" (an alternate data stream), without trailing dots and spaces.
func winName(p string) string {
	b := winBase(p)
	if i := strings.IndexByte(b, ':'); i >= 0 {
		b = b[:i]
	}
	return strings.TrimRight(b, ". ")
}

func winSlash(p string) string { return strings.ReplaceAll(p, "/", `\`) }

func winBase(p string) string {
	p = winSlash(p)
	if i := strings.LastIndexByte(p, '\\'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func winDir(p string) string {
	p = winSlash(p)
	if i := strings.LastIndexByte(p, '\\'); i >= 0 {
		return p[:i]
	}
	return "."
}

// winJoinInside joins rel onto dir and reports whether the result stays
// inside dir (no "..", no absolute or drive-relative rel).
func winJoinInside(dir, rel string) (string, bool) {
	rel = winSlash(rel)
	if strings.HasPrefix(rel, `\`) || strings.Contains(rel, ":") {
		return "", false
	}
	var parts []string
	for _, el := range strings.Split(rel, `\`) {
		switch el {
		case "", ".":
		case "..":
			return "", false
		default:
			parts = append(parts, el)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.TrimRight(winSlash(dir), `\`) + `\` + strings.Join(parts, `\`), true
}
