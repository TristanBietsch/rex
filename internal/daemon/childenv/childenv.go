// Package childenv turns a parent process environment into the environment a
// child process (an agent, a model probe, a loop check) receives.
//
// The default policy is an allowlist: the child gets a fixed set of base
// names (paths, locale, proxies, terminal and OS essentials), plus whatever
// names the caller passes explicitly, and nothing else. This keeps API keys
// and tokens exported in the daemon's shell from reaching every child for the
// daemon's whole lifetime. The alternative policy, inherit, keeps everything.
//
// The allowlist is not a sandbox. Anything passed through is fully visible to
// the child, and on some systems a process can read another same-user
// process's environment. It limits what is handed over by default; it does not
// stop a child from finding secrets by other means.
//
// Build is pure: it never reads os.Environ or any other global state, so the
// caller decides which parent environment to filter.
package childenv

import (
	"runtime"
	"slices"
	"strings"
)

// Mode selects how Build treats the parent environment.
type Mode string

const (
	// ModeAllowlist keeps only base names and explicitly passed names.
	ModeAllowlist Mode = "allowlist"
	// ModeInherit keeps the whole parent environment (minus the names that
	// are always removed).
	ModeInherit Mode = "inherit"
)

// sessionMarkers are the variables that mark a running Claude session. They
// make a nested Claude misbehave, so they are removed in every mode.
var sessionMarkers = []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT"}

// filteredMarker is set by rex on a daemon whose environment is already
// filtered. It is rex's own bookkeeping and is never passed to children.
const filteredMarker = "REX_DAEMON_ENV_FILTERED"

// Options configures Build.
type Options struct {
	// Mode is the policy. Any value other than ModeInherit is treated as
	// ModeAllowlist, so a bad setting fails closed.
	Mode Mode
	// Pass lists extra names to keep in allowlist mode. A name ending in "*"
	// matches every variable with that prefix. Ignored in inherit mode.
	Pass []string
	// SSHAgent keeps SSH_AUTH_SOCK in allowlist mode.
	SSHAgent bool
	// GOOS selects the base names and name matching rules. Empty means
	// runtime.GOOS.
	GOOS string
}

var baseAll = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TERM", "COLORTERM", "LANG", "LC_*", "TZ", "TMPDIR", "XDG_*",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "EDITOR", "VISUAL",
}

var baseDarwin = []string{"__CF_USER_TEXT_ENCODING"}

var baseLinux = []string{"DISPLAY", "WAYLAND_DISPLAY", "DBUS_SESSION_BUS_ADDRESS"}

var baseWindows = []string{
	"SystemRoot", "SystemDrive", "windir", "COMSPEC", "PATHEXT", "USERPROFILE", "USERNAME", "APPDATA",
	"LOCALAPPDATA", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "CommonProgramFiles",
	"TEMP", "TMP", "HOMEDRIVE", "HOMEPATH", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE", "OS",
}

// BaseNames returns the base allowlist for goos (empty means runtime.GOOS).
// SSH_AUTH_SOCK is included only when sshAgent is true. Names ending in "*"
// are prefix patterns. The result is a fresh slice the caller may modify.
func BaseNames(goos string, sshAgent bool) []string {
	if goos == "" {
		goos = runtime.GOOS
	}
	names := slices.Clone(baseAll)
	if sshAgent {
		names = append(names, "SSH_AUTH_SOCK")
	}
	switch goos {
	case "darwin":
		names = append(names, baseDarwin...)
	case "linux":
		names = append(names, baseLinux...)
	case "windows":
		names = append(names, baseWindows...)
	}
	return names
}

// Build returns the child environment for parent as KEY=VALUE entries, in the
// parent's order. The session markers and the daemon-filtered marker are
// removed in every mode, even when a Pass pattern would match them. Entries
// without "=" or with an empty name are dropped; the one exception is a
// Windows per-drive entry such as "=C:=C:\", which inherit mode keeps.
// Neither parent nor o.Pass is modified.
func Build(parent []string, o Options) []string {
	goos := o.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	fold := goos == "windows"
	inherit := o.Mode == ModeInherit
	var allow []string
	if !inherit {
		allow = slices.Concat(BaseNames(goos, o.SSHAgent), o.Pass)
	}
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if name == "" {
			if inherit && fold && strings.Contains(kv[1:], "=") {
				out = append(out, kv)
			}
			continue
		}
		if matchesAny(name, sessionMarkers, fold) || matchesAny(name, []string{filteredMarker}, fold) {
			continue
		}
		if inherit || matchesAny(name, allow, fold) {
			out = append(out, kv)
		}
	}
	return out
}

// matchesAny reports whether name equals one of patterns or, for a pattern
// ending in "*", starts with the pattern's prefix. fold ignores case. An empty
// pattern and a bare "*" match nothing.
func matchesAny(name string, patterns []string, fold bool) bool {
	if fold {
		name = strings.ToLower(name)
	}
	for _, p := range patterns {
		if fold {
			p = strings.ToLower(p)
		}
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			// A bare "*" would match everything; it matches nothing.
			if prefix != "" && strings.HasPrefix(name, prefix) {
				return true
			}
		} else if p != "" && name == p {
			return true
		}
	}
	return false
}
