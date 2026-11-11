package usage

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// ErrNoGitBash is returned on Windows when Git for Windows' bash is not found.
var ErrNoGitBash = errors.New("git bash not found (set CLAUDE_CODE_GIT_BASH_PATH)")

// shellFor is the command prefix that runs a shell command string: sh -c, or
// on Windows the Git for Windows bash -c, found by FindGitBash. bash is never
// taken from PATH there: it can be the WSL launcher in System32.
func shellFor(goos string) ([]string, error) {
	if goos != "windows" {
		return []string{"sh", "-c"}, nil
	}
	bash, err := FindGitBash(os.Getenv, fileExists, gitExecPath, gitLocation)
	if err != nil {
		return nil, err
	}
	return []string{bash, "-c"}, nil
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// gitExecPath is the output of `git --exec-path`, "" when it fails.
func gitExecPath() string {
	out, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitLocation is where git.exe is on PATH, "" when not found.
func gitLocation() string {
	p, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	return p
}

// FindGitBash locates Git for Windows' bash.exe, the shell Claude Code uses
// for its commands on Windows. In order: $CLAUDE_CODE_GIT_BASH_PATH; the
// install root implied by `git --exec-path` (<root>\mingw64\libexec\git-core)
// and by git.exe's own location (<root>\cmd\git.exe), each with bin\bash.exe
// and usr\bin\bash.exe. A path inside System32 is never returned (that is
// the WSL launcher). The inputs are injected so it is testable anywhere.
func FindGitBash(getenv func(string) string, exists func(string) bool, execPath, gitExe func() string) (string, error) {
	ok := func(p string) bool { return p != "" && !inSystem32(p) && exists(p) }
	if p := getenv("CLAUDE_CODE_GIT_BASH_PATH"); p != "" && ok(p) {
		return p, nil
	}
	var roots []string
	if ep := execPath(); ep != "" {
		roots = append(roots, up(ep, 3)) // git-core, libexec, mingw64
		roots = append(roots, up(ep, 2)) // older layouts: <root>\libexec\git-core
	}
	if ge := gitExe(); ge != "" {
		roots = append(roots, up(ge, 2)) // git.exe, cmd
	}
	for _, r := range roots {
		if r == "" {
			continue
		}
		for _, rel := range []string{`\bin\bash.exe`, `\usr\bin\bash.exe`} {
			if p := r + rel; ok(p) {
				return p, nil
			}
		}
	}
	return "", ErrNoGitBash
}

// up drops the last n elements of a Windows or slash-separated path.
func up(p string, n int) string {
	p = strings.TrimRight(p, `\/`)
	for range n {
		i := strings.LastIndexAny(p, `\/`)
		if i < 0 {
			return ""
		}
		p = p[:i]
	}
	return p
}

func inSystem32(p string) bool {
	l := strings.ToLower(strings.ReplaceAll(p, "/", `\`))
	return strings.Contains(l, `\system32\`)
}
