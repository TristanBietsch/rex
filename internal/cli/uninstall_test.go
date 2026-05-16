package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripBlockFromFile_basic(t *testing.T) {
	input := `export PATH="$HOME/.local/bin:$PATH"
# BEGIN REX
export REX_SOCKET=/tmp/rex.sock
alias rex="rex --color"
# END REX
# some other config
export EDITOR=vim
`
	want := `export PATH="$HOME/.local/bin:$PATH"
# some other config
export EDITOR=vim
`
	modified, content := runStrip(t, input)
	if !modified {
		t.Fatal("expected modified=true")
	}
	if content != want {
		t.Fatalf("got:\n%q\nwant:\n%q", content, want)
	}
}

func TestStripBlockFromFile_caseInsensitive(t *testing.T) {
	input := "before\n# Begin Rex\nstuff\n# End Rex\nafter\n"
	want := "before\nafter\n"
	modified, content := runStrip(t, input)
	if !modified {
		t.Fatal("expected modified=true")
	}
	if content != want {
		t.Fatalf("got:\n%q\nwant:\n%q", content, want)
	}
}

func TestStripBlockFromFile_noBlock(t *testing.T) {
	input := "no rex block here\nexport FOO=bar\n"
	modified, content := runStrip(t, input)
	if modified {
		t.Fatal("expected modified=false when no block present")
	}
	// content is unchanged because stripBlockFromFile returns early.
	_ = content
}

func TestStripBlockFromFile_multipleBlocks(t *testing.T) {
	input := "a\n# BEGIN REX\nb\n# END REX\nc\n# BEGIN REX\nd\n# END REX\ne\n"
	want := "a\nc\ne\n"
	modified, content := runStrip(t, input)
	if !modified {
		t.Fatal("expected modified=true")
	}
	if content != want {
		t.Fatalf("got:\n%q\nwant:\n%q", content, want)
	}
}

func TestStripBlockFromFile_nonexistent(t *testing.T) {
	modified, err := stripBlockFromFile("/nonexistent/path/to/file.sh")
	if err != nil {
		t.Fatalf("unexpected error for nonexistent file: %v", err)
	}
	if modified {
		t.Fatal("expected modified=false for nonexistent file")
	}
}

func TestHasProfileBlock(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".zshrc")
	_ = os.WriteFile(f, []byte("# BEGIN REX\nfoo\n# END REX\n"), 0o644)
	if !hasProfileBlock(f) {
		t.Fatal("expected block to be detected")
	}
	_ = os.WriteFile(f, []byte("no block\n"), 0o644)
	if hasProfileBlock(f) {
		t.Fatal("expected no block detected")
	}
}

// runStrip writes input to a temp file, calls stripBlockFromFile, and returns
// modified flag + resulting file content.
func runStrip(t *testing.T, input string) (bool, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "profile")
	if err := os.WriteFile(p, []byte(input), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	modified, err := stripBlockFromFile(p)
	if err != nil {
		t.Fatalf("stripBlockFromFile: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	return modified, strings.ReplaceAll(string(data), "\r\n", "\n")
}
