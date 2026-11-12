package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// cliSources parses every non-test Go file under internal/surface/cli, keyed
// by its path relative to that folder.
func cliSources(t *testing.T) map[string]*ast.File {
	t.Helper()
	root := ".."
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = f
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)
	return files
}

var hexColor = regexp.MustCompile(`^"#[0-9a-fA-F]{3,8}"$`)

// Colors come from the theme palette: no hex literal in the CLI.
func TestNoHardcodedHex(t *testing.T) {
	for name, f := range cliSources(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && hexColor.MatchString(lit.Value) {
				t.Errorf("%s: hardcoded color %s; use the theme palette (core.Styles)", name, lit.Value)
			}
			return true
		})
	}
}

// directStdoutAllowed are the files that own a terminal or build the process
// Printer. Everything else prints through core.Out().
var directStdoutAllowed = map[string]string{
	"core/printer.go":         "builds the process Printer on the real streams",
	"core/term.go":            "terminal width of stdout",
	"session/attach.go":       "attach owns the raw terminal (alt screen, byte passthrough)",
	"session/term_windows.go": "attach's console mode",
}

// A command renders through the Printer: it never calls fmt.Print* or
// touches os.Stdout/os.Stderr itself, so every format and stream rule holds.
func TestNoDirectStdout(t *testing.T) {
	for name, f := range cliSources(t) {
		if _, ok := directStdoutAllowed[name]; ok {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch {
			case id.Name == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr"):
				t.Errorf("%s: os.%s used directly; write through core.Out()", name, sel.Sel.Name)
			case id.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Print"):
				t.Errorf("%s: fmt.%s used directly; render through core.Out().Emit", name, sel.Sel.Name)
			}
			return true
		})
	}
}
