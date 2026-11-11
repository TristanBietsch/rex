package core

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

const longA = "alpha-alpha-alpha-alpha-alpha"

func layoutTable() Table {
	return Table{Kind: "r", Cols: []Col{
		{Key: "id", Title: "ID"},
		{Key: "extra", Title: "Extra", DropBelow: 60},
		{Key: "slug", Title: "Slug", Flex: true, Min: 6},
		{Key: "a", Title: "A", Below: true},
		{Key: "b", Title: "B", Below: true},
	}, Rows: [][]Cell{
		{{Text: "1"}, {Text: "xx"}, {Text: "a-long-slug-name"}, {Text: longA}, {Text: ""}},
		{{Text: "2"}, {Text: "yy"}, {Text: "short"}, {Text: ""}, {Text: ""}},
		{{Text: "3"}, {Text: "zz"}, {Text: "s"}, {Text: "gamma"}, {Text: "delta"}},
	}}
}

func TestTableBelowColumnsMoveToSecondLine(t *testing.T) {
	// Wide: everything is a column.
	wide := emitKit(t, "text", 120, false, Result{View: View{layoutTable()}})
	require.Contains(t, wide, "ID  EXTRA  SLUG")
	require.Contains(t, wide, "1   xx     a-long-slug-name  "+longA)

	// Narrow: Extra is dropped, the Below columns hang under the row, and a
	// row with no Below text has no second line.
	narrow := emitKit(t, "text", 40, false, Result{View: View{layoutTable()}})
	lines := strings.Split(strings.TrimSuffix(narrow, "\n"), "\n")
	require.Equal(t, []string{
		"  ID  SLUG",
		"  1   a-long-slug-name",
		"      " + longA,
		"  2   short",
		"  3   s",
		"      gamma  delta",
	}, lines)
	for _, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), 40, l)
	}
}

func TestTableBelowMovesLastColumnFirst(t *testing.T) {
	tb := layoutTable()
	tb.Cols[3].Below = false // only B may move
	out := emitKit(t, "text", 40, false, Result{View: View{tb}})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Regexp(t, `^  ID  SLUG +A$`, lines[0], "A stays a column, B moved")
	require.Equal(t, "      delta", lines[len(lines)-1])
}

func TestTableDropBelowAppliesToTextOnly(t *testing.T) {
	require.NotContains(t, emitKit(t, "text", 50, false, Result{View: View{layoutTable()}}), "EXTRA")
	require.Contains(t, emitKit(t, "txt", 50, false, Result{View: View{layoutTable()}}), "EXTRA")
	require.Contains(t, emitKit(t, "md", 50, false, Result{View: View{layoutTable()}}), "Extra")
}
