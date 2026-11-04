package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRewriteShellBlockIdempotent verifies that calling rewriteShellBlock twice
// produces the same content as calling it once (no duplication).
func TestRewriteShellBlockIdempotent(t *testing.T) {
	block := ShellProfileBlock("bash")
	initial := "# my custom stuff\nexport FOO=bar\n"

	once := rewriteShellBlock(initial, block)
	twice := rewriteShellBlock(once, block)

	if once != twice {
		t.Errorf("rewriteShellBlock is not idempotent:\nfirst  = %q\nsecond = %q", once, twice)
	}
}

// TestRewriteShellBlockPreservesOutsideContent verifies that content outside the
// BEGIN/END markers is preserved exactly when the block is rewritten.
func TestRewriteShellBlockPreservesOutsideContent(t *testing.T) {
	block := ShellProfileBlock("bash")
	before := "export BEFORE=1\n"
	after := "export AFTER=2\n"
	initial := before + block + "\n" + after

	result := rewriteShellBlock(initial, block)

	if !strings.HasPrefix(result, before) {
		t.Errorf("content before block not preserved; got:\n%s", result)
	}
	if !strings.HasSuffix(result, after) {
		t.Errorf("content after block not preserved; got:\n%s", result)
	}
	// block itself must appear exactly once
	if count := strings.Count(result, shellBlockBegin); count != 1 {
		t.Errorf("BEGIN marker appears %d times, want 1:\n%s", count, result)
	}
	if count := strings.Count(result, shellBlockEnd); count != 1 {
		t.Errorf("END marker appears %d times, want 1:\n%s", count, result)
	}
}

// TestRewriteShellBlockAppend verifies that the block is appended when no
// existing markers are present.
func TestRewriteShellBlockAppend(t *testing.T) {
	block := ShellProfileBlock("zsh")
	content := "export EXISTING=1\n"
	result := rewriteShellBlock(content, block)

	if !strings.Contains(result, shellBlockBegin) {
		t.Error("expected BEGIN marker in appended result")
	}
	if !strings.Contains(result, "export EXISTING=1") {
		t.Error("pre-existing content should be preserved on append")
	}
}

// TestInstallShellBlockIdempotent writes the block to a temp file twice and
// confirms the file content is identical after each call.
func TestInstallShellBlockIdempotent(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	// seed with some pre-existing content
	if err := os.WriteFile(rc, []byte("export SEED=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Override SHELL env so detectShellRC in installShellBlock picks "zsh".
	t.Setenv("SHELL", "/bin/zsh")

	if err := installShellBlock(rc); err != nil {
		t.Fatalf("first install: %v", err)
	}
	first, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}

	if err := installShellBlock(rc); err != nil {
		t.Fatalf("second install: %v", err)
	}
	second, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != string(second) {
		t.Errorf("install is not idempotent:\nfirst  = %q\nsecond = %q", string(first), string(second))
	}
}

// TestHasShellBlock confirms detection works correctly.
func TestHasShellBlock(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bashrc")

	if hasShellBlock(rc) {
		t.Error("expected false for non-existent file")
	}

	if err := os.WriteFile(rc, []byte("export FOO=bar\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if hasShellBlock(rc) {
		t.Error("expected false before block installed")
	}

	block := ShellProfileBlock("bash")
	content := rewriteShellBlock("export FOO=bar\n", block)
	if err := os.WriteFile(rc, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasShellBlock(rc) {
		t.Error("expected true after block installed")
	}
}

// TestShellProfileBlock verifies the block contains the required markers and
// the PATH export line.
func TestShellProfileBlock(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		block := ShellProfileBlock(shell)
		if !strings.Contains(block, shellBlockBegin) {
			t.Errorf("shell=%s: missing BEGIN marker", shell)
		}
		if !strings.Contains(block, shellBlockEnd) {
			t.Errorf("shell=%s: missing END marker", shell)
		}
		if !strings.Contains(block, `$HOME/.local/bin`) {
			t.Errorf("shell=%s: missing PATH export", shell)
		}
	}
}

// withUserTools points HOME at a fresh dir whose ~/.config/rex/tools.yaml
// holds yaml, and returns that file's path.
func withUserTools(t *testing.T, yaml string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".config", "rex", "tools.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var brokenToolsYAML = map[string]string{
	"parse error":      "tools: [\n",
	"validation error": "tools:\n  - id: \"\"\n",
}

// A broken ~/.config/rex/tools.yaml made registry.Load return nil, and
// `rex setup` dereferenced it (backlog P0-08). The wizard must fall back to
// the built-in tools and tell the user why.
func TestSetupWizard_BrokenToolsYAMLFallsBackToBuiltin(t *testing.T) {
	for name, yaml := range brokenToolsYAML {
		t.Run(name, func(t *testing.T) {
			withUserTools(t, yaml)
			m := NewSetupWizardModel(filepath.Join(t.TempDir(), "config.yaml"), "")
			if len(m.tools) == 0 {
				t.Fatal("no tools offered; want the built-in registry")
			}
			if m.toolsErr == nil {
				t.Fatal("tools.yaml error not recorded")
			}
			for _, step := range []setupStep{setupStepWelcome, setupStepDefaults} {
				m.step = step
				view := ansi.Strip(m.View())
				if !strings.Contains(view, "tools.yaml") || !strings.Contains(view, "built-in") {
					t.Errorf("step %d does not explain the fallback:\n%s", step, view)
				}
			}
		})
	}
}

// A valid tools.yaml shows no warning.
func TestSetupWizard_ValidToolsYAMLNoWarning(t *testing.T) {
	withUserTools(t, "tools: []\n")
	m := NewSetupWizardModel(filepath.Join(t.TempDir(), "config.yaml"), "")
	if m.toolsErr != nil {
		t.Fatalf("unexpected tools error: %v", m.toolsErr)
	}
	if strings.Contains(ansi.Strip(m.View()), "tools.yaml") {
		t.Error("warning shown for a valid tools.yaml")
	}
}

// The boot step used to validate only the built-in registry, so a broken
// user tools.yaml passed boot and failed later. It now loads the user file
// and reports its error as a warning (the board still works on builtins).
func TestStepRegistryLoad_ValidatesUserToolsYAML(t *testing.T) {
	for name, yaml := range brokenToolsYAML {
		t.Run(name, func(t *testing.T) {
			withUserTools(t, yaml)
			msg, ok := stepRegistryLoad(Model{})().(bootStepMsg)
			if !ok {
				t.Fatal("not a bootStepMsg")
			}
			if msg.Status != stepWarn || msg.Err == nil {
				t.Fatalf("status=%v err=%v; want warn with the load error", msg.Status, msg.Err)
			}
			if !strings.Contains(msg.Desc, "tools.yaml") || !strings.Contains(msg.Desc, msg.Err.Error()) {
				t.Errorf("desc %q does not name tools.yaml", msg.Desc)
			}
		})
	}
	t.Run("valid", func(t *testing.T) {
		withUserTools(t, "tools: []\n")
		msg, _ := stepRegistryLoad(Model{})().(bootStepMsg)
		if msg.Status != stepOK {
			t.Fatalf("status=%v desc=%q; want ok", msg.Status, msg.Desc)
		}
	})
}
