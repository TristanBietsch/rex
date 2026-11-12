package winlaunch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Payloads that are cmd.exe syntax when they reach it.
var injectionPayloads = []string{
	"fix it & calc",
	"x | whoami",
	"%PATH%",
	"!PATH!",
	"a ^& b",
	`say "hi" & calc`,
	`trailing \" & calc`,
	`\\" & whoami & "`,
	"line one\r\nwhoami",
	"line one\ncalc",
	"> out.txt",
	"< in.txt",
	"(calc)",
	"\x1a eof",
	"fullwidth \uff02 & calc",
}

const npmShim7 = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0

IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
  SET PATHEXT=%PATHEXT:;.JS;=;%
)

endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\@anthropic-ai\claude-code\cli.js" %*
`

const npmShim6 = `@IF EXIST "%~dp0\node.exe" (
  "%~dp0\node.exe"  "%~dp0\node_modules\@qwen-code\qwen-code\cli.js" %*
) ELSE (
  @SETLOCAL
  @SET PATHEXT=%PATHEXT:;.JS;=;%
  node  "%~dp0\node_modules\@qwen-code\qwen-code\cli.js" %*
)
`

const npmShimExe = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0
"%dp0%\node_modules\@openai\codex\bin\codex.exe"   %*
`

// fakeFS is a Windows file system for Plan.
type fakeFS struct {
	path  map[string]string // command name -> resolved path
	files map[string]string // path -> content
}

func (f fakeFS) env() Env {
	return Env{
		LookPath: func(name string) (string, error) {
			if p, ok := f.path[strings.ToLower(name)]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		},
		ReadFile: func(p string) ([]byte, error) {
			if c, ok := f.files[p]; ok {
				return []byte(c), nil
			}
			return nil, errors.New("not found")
		},
		IsFile: func(p string) bool { _, ok := f.files[p]; return ok },
	}
}

const npmDir = `C:\Users\u\AppData\Roaming\npm`

func npmFS(shim string) fakeFS {
	return fakeFS{
		path: map[string]string{"claude": npmDir + `\claude.cmd`, "node": `C:\Program Files\nodejs\node.exe`},
		files: map[string]string{
			npmDir + `\claude.cmd`: shim,
			npmDir + `\node_modules\@anthropic-ai\claude-code\cli.js`: "",
			npmDir + `\node_modules\@qwen-code\qwen-code\cli.js`:      "",
			npmDir + `\node_modules\@openai\codex\bin\codex.exe`:      "",
			`C:\Program Files\nodejs\node.exe`:                        "",
		},
	}
}

func TestIsBatch(t *testing.T) {
	for _, p := range []string{`C:\x\claude.cmd`, `C:\x\CLAUDE.CMD`, `run.bat`, `C:/x/y.Bat`} {
		require.True(t, IsBatch(p), p)
	}
	for _, p := range []string{`C:\x\claude.exe`, `claude`, `C:\x.cmd\claude.exe`, `cmd`, ``} {
		require.False(t, IsBatch(p), p)
	}
	require.True(t, IsCmdShell(`C:\Windows\System32\CMD.EXE`))
	require.True(t, IsCmdShell("cmd"))
	require.False(t, IsCmdShell(`C:\x\cmdx.exe`))
}

func TestSafeArg(t *testing.T) {
	for _, s := range []string{"--model", "opus", "--effort=high", `C:\Users\u\.rex\s\plugin`, "a b", "x,y", "@file", "~/x", "#1", "a+b"} {
		require.True(t, SafeArg(s), s)
	}
	for _, s := range append([]string{"", `C:\dir with space\`, "a;b", "a'b", "a`b", "é", "{\"a\":1}", "a\tb"}, injectionPayloads...) {
		require.False(t, SafeArg(s), "%q", s)
	}
}

func TestParseShim(t *testing.T) {
	cases := []struct {
		name, text, target string
		node, ok           bool
	}{
		{"npm7", npmShim7, `node_modules\@anthropic-ai\claude-code\cli.js`, true, true},
		{"npm6", npmShim6, `node_modules\@qwen-code\qwen-code\cli.js`, true, true},
		{"exe", npmShimExe, `node_modules\@openai\codex\bin\codex.exe`, false, true},
		{"hand-written", "@echo off\r\npython %~dp0\\tool.py %*\r\n", "", false, false},
		{"two targets", `"%dp0%\a.js" %*` + "\n" + `"%dp0%\b.js" %*`, "", false, false},
		{"ps1 target", `"%dp0%\x.ps1" %*`, "", false, false},
		{"drive in target", `"%dp0%\C:\x.js" %*`, "", false, false},
		{"empty", "", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target, node, ok := ParseShim(c.text)
			require.Equal(t, c.ok, ok)
			require.Equal(t, c.target, target)
			require.Equal(t, c.node, node)
		})
	}
}

func TestWinJoinInside(t *testing.T) {
	p, ok := winJoinInside(`C:\npm\`, `node_modules\.\x\cli.js`)
	require.True(t, ok)
	require.Equal(t, `C:\npm\node_modules\x\cli.js`, p)
	for _, rel := range []string{`..\x.js`, `a\..\..\x.js`, `\x.js`, `C:x.js`, `.`, ``} {
		_, ok := winJoinInside(`C:\npm`, rel)
		require.False(t, ok, rel)
	}
}

// An npm shim is resolved to node and the script (or the package's exe):
// the prompt stays on argv, byte for byte, and cmd.exe never runs.
func TestPlanResolvesNpmShim(t *testing.T) {
	for _, p := range injectionPayloads {
		fs := npmFS(npmShim7)
		l, err := Plan(Request{Base: []string{"claude", "--model", "opus"}, PromptArgs: []string{"--", p}, Prompt: p}, fs.env())
		require.NoError(t, err)
		require.Equal(t, "shim:node", l.Via)
		require.Equal(t, []string{`C:\Program Files\nodejs\node.exe`, npmDir + `\node_modules\@anthropic-ai\claude-code\cli.js`, "--model", "opus", "--", p}, l.Argv)
		require.False(t, l.PromptMoved)
		require.Empty(t, l.Paste)
		for _, a := range l.Argv[:2] {
			require.False(t, IsBatch(a))
		}
	}

	fs := npmFS(npmShim6)
	fs.files[npmDir+`\node.exe`] = ""
	l, err := Plan(Request{Base: []string{"claude"}}, fs.env())
	require.NoError(t, err)
	require.Equal(t, npmDir+`\node.exe`, l.Argv[0], "node.exe next to the shim wins, as in the shim")

	l, err = Plan(Request{Base: []string{"claude"}, PromptArgs: []string{"x & calc"}, Prompt: "x & calc"}, npmFS(npmShimExe).env())
	require.NoError(t, err)
	require.Equal(t, "shim:exe", l.Via)
	require.Equal(t, []string{npmDir + `\node_modules\@openai\codex\bin\codex.exe`, "x & calc"}, l.Argv)
}

// A shim whose target is missing, or which needs a node that is itself a
// batch file, is not resolved.
func TestPlanUnresolvedShimMovesPromptToPaste(t *testing.T) {
	cases := map[string]fakeFS{}
	missing := npmFS(npmShim7)
	delete(missing.files, npmDir+`\node_modules\@anthropic-ai\claude-code\cli.js`)
	cases["missing target"] = missing
	batchNode := npmFS(npmShim7)
	batchNode.path["node"] = `C:\x\node.cmd`
	cases["node is a batch file"] = batchNode
	noNode := npmFS(npmShim7)
	delete(noNode.path, "node")
	cases["no node"] = noNode
	hand := npmFS("@echo off\r\nnode %~dp0\\cli.js %*\r\n")
	cases["hand-written shim"] = hand
	escape := npmFS(`"%dp0%\..\evil\cli.js" %*`)
	escape.files[`C:\Users\u\AppData\Roaming\evil\cli.js`] = ""
	cases["target outside the shim folder"] = escape

	for name, fs := range cases {
		for _, p := range injectionPayloads {
			l, err := Plan(Request{Base: []string{"claude", "--model", "opus"}, PromptArgs: []string{"--", p}, Prompt: p}, fs.env())
			require.NoError(t, err, name)
			require.Equal(t, "shim:safe-args", l.Via, name)
			require.Equal(t, []string{npmDir + `\claude.cmd`, "--model", "opus"}, l.Argv, "%s: the prompt never reaches cmd.exe", name)
			require.True(t, l.PromptMoved)
			require.Equal(t, p, l.Paste)
			for _, a := range l.Argv {
				require.True(t, SafeArg(a), "%s: %q", name, a)
			}
		}
	}
}

// An unresolved shim with any argument cmd.exe would interpret is refused,
// naming the shim.
func TestPlanRefusesUnsafeShimArgs(t *testing.T) {
	fs := npmFS("@echo off\r\nnode %~dp0\\cli.js %*\r\n")
	for _, p := range injectionPayloads {
		_, err := Plan(Request{Base: []string{"claude", "--settings", p}}, fs.env())
		require.ErrorIs(t, err, ErrRefused)
		require.ErrorIs(t, err, errUnsafeArgs)
		require.Contains(t, err.Error(), `claude.cmd`)
	}
	amp := fakeFS{path: map[string]string{"t": `C:\a&b\t.cmd`}, files: map[string]string{`C:\a&b\t.cmd`: "@echo off"}}
	_, err := Plan(Request{Base: []string{"t"}}, amp.env())
	require.ErrorIs(t, err, ErrRefused, "the shim's own path is on cmd.exe's command line too")
}

// cmd.exe as the registry command keeps its own arguments but never gets
// the prompt.
func TestPlanCmdShellNeverGetsPrompt(t *testing.T) {
	fs := fakeFS{path: map[string]string{"cmd": `C:\Windows\System32\cmd.exe`}, files: map[string]string{}}
	l, err := Plan(Request{Base: []string{"cmd", "/c", "tool.cmd"}, PromptArgs: []string{"x & calc"}, Prompt: "x & calc"}, fs.env())
	require.NoError(t, err)
	require.Equal(t, "cmd.exe", l.Via)
	require.Equal(t, []string{`C:\Windows\System32\cmd.exe`, "/c", "tool.cmd"}, l.Argv)
	require.Equal(t, "x & calc", l.Paste)
}

// A native exe keeps everything on argv: Go quotes each element for the C
// runtime, and no cmd.exe is involved.
func TestPlanDirectExe(t *testing.T) {
	fs := fakeFS{path: map[string]string{"claude": `C:\bin\claude.exe`}, files: map[string]string{}}
	for _, p := range injectionPayloads {
		l, err := Plan(Request{Base: []string{"claude"}, PromptArgs: []string{"--", p}, Prompt: p}, fs.env())
		require.NoError(t, err)
		require.Equal(t, "direct", l.Via)
		require.Equal(t, []string{`C:\bin\claude.exe`, "--", p}, l.Argv)
	}
	_, err := Plan(Request{Base: []string{"nope"}}, fs.env())
	require.ErrorIs(t, err, exec.ErrNotFound)
	_, err = Plan(Request{}, fs.env())
	require.Error(t, err)
}

// A prompt bound for the paste path (prompt_paste) passes through.
func TestPlanKeepsPastePrompt(t *testing.T) {
	fs := fakeFS{path: map[string]string{"ollama": `C:\bin\ollama.exe`}, files: map[string]string{}}
	l, err := Plan(Request{Base: []string{"ollama", "run", "llama3"}, Prompt: "hi & calc"}, fs.env())
	require.NoError(t, err)
	require.Equal(t, "hi & calc", l.Paste)
	require.False(t, l.PromptMoved)
	require.Equal(t, []string{`C:\bin\ollama.exe`, "run", "llama3"}, l.Argv)
}

// Over MaxCmdLine the prompt moves to the paste path; a command line too
// long without it is refused.
func TestPlanLengthCap(t *testing.T) {
	fs := fakeFS{path: map[string]string{"claude": `C:\bin\claude.exe`}, files: map[string]string{}}
	fits := strings.Repeat("a", MaxCmdLine-100)
	l, err := Plan(Request{Base: []string{"claude"}, PromptArgs: []string{"--", fits}, Prompt: fits}, fs.env())
	require.NoError(t, err)
	require.False(t, l.PromptMoved)

	big := strings.Repeat("a", 40000)
	l, err = Plan(Request{Base: []string{"claude"}, PromptArgs: []string{"--", big}, Prompt: big}, fs.env())
	require.NoError(t, err)
	require.True(t, l.PromptMoved)
	require.Equal(t, "length", l.Reason)
	require.Equal(t, big, l.Paste)
	require.Equal(t, []string{`C:\bin\claude.exe`}, l.Argv)

	// UTF-16 units count, and quotes and backslashes may double.
	quotes := strings.Repeat(`\"`, MaxCmdLine/3)
	require.Greater(t, CmdLineBound([]string{quotes}), MaxCmdLine)
	astral := strings.Repeat("\U0001F600", MaxCmdLine/2)
	require.Greater(t, CmdLineBound([]string{astral}), MaxCmdLine)

	_, err = Plan(Request{Base: []string{"claude", big}}, fs.env())
	require.ErrorIs(t, err, ErrRefused)
	_, err = Plan(Request{Base: []string{"claude", big}, PromptArgs: []string{"x"}, Prompt: "x"}, fs.env())
	require.ErrorIs(t, err, ErrRefused)
}

func TestCmdLineBound(t *testing.T) {
	require.Equal(t, 0, CmdLineBound(nil))
	require.Equal(t, 4, CmdLineBound([]string{"a"}))
	require.Equal(t, 5+1+2+3, CmdLineBound([]string{`a\"b"`}))
}

func TestSystemEnv(t *testing.T) {
	dir := t.TempDir()
	env := SystemEnv()
	require.False(t, env.IsFile(dir))
	_, err := env.ReadFile(dir + "/missing")
	require.Error(t, err)

	shim := filepath.Join(dir, "big.cmd")
	require.NoError(t, os.WriteFile(shim, []byte(strings.Repeat("x", maxShimBytes+10)), 0o600))
	require.True(t, env.IsFile(shim))
	b, err := env.ReadFile(shim)
	require.NoError(t, err)
	require.Len(t, b, maxShimBytes+1, "read stops one byte past the cap")

	// An oversized shim is not parsed.
	fs := npmFS(npmShim7 + strings.Repeat(" ", maxShimBytes))
	l, err := Plan(Request{Base: []string{"claude"}}, fs.env())
	require.NoError(t, err)
	require.Equal(t, "shim:safe-args", l.Via)
	require.Equal(t, ".", winDir("claude.cmd"))
}
