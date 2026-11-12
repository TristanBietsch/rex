package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var hexColorLit = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)

// Colors live in internal/catalog/theme and reach the TUI through the
// palette tokens in styles.go; no TUI source file names one.
func TestNoHardcodedHex(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f, nil, 0)
		require.NoError(t, err)
		checked++
		ast.Inspect(parsed, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if v, err := strconv.Unquote(lit.Value); err == nil && hexColorLit.MatchString(v) {
				t.Errorf("%s: hex color literal %s; add a theme token instead", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
	require.Positive(t, checked)
}

// No renderer builds a colored style inline: styles are prebuilt from the
// palette (rebuildStyles), so a theme change reaches every one of them.
func TestNoInlineColoredStyles(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "styles.go" {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "lipgloss.NewStyle()") && strings.Contains(line, "Foreground(") {
				t.Errorf("%s:%d: inline colored style; use a prebuilt style from styles.go", f, i+1)
			}
		}
	}
}
