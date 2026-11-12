package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestComposer_InsertMultiline(t *testing.T) {
	var c Composer
	c.Insert("a\nb")
	require.Equal(t, "a\nb", c.String())
	require.Equal(t, 1, c.Row)
	require.Equal(t, 1, c.Col)
	require.False(t, c.Empty())

	// \r\n and a lone \r are newlines; escapes and other controls are dropped.
	var d Composer
	d.Insert("x\r\ny\rz\x1b[31mred\x1b[0m\x07‮!")
	require.Equal(t, "x\ny\nzred!", d.String())
}

func TestComposer_EmptyAndClear(t *testing.T) {
	var c Composer
	require.True(t, c.Empty())
	require.Empty(t, c.String())
	c.Insert("one\ntwo")
	c.Clear()
	require.True(t, c.Empty())
	require.Equal(t, 0, c.Row)
	require.Equal(t, 0, c.Col)
}

func TestComposer_BackspaceJoinsLines(t *testing.T) {
	var c Composer
	c.Insert("ab\ncd")
	c.Home()
	c.Backspace()
	require.Equal(t, "abcd", c.String())
	require.Equal(t, 0, c.Row)
	require.Equal(t, 2, c.Col, "the cursor stays where the lines met")

	c.Backspace()
	require.Equal(t, "acd", c.String())
	c.Home()
	c.Backspace() // nothing before the first rune
	require.Equal(t, "acd", c.String())
}

func TestComposer_NewlineSplits(t *testing.T) {
	var c Composer
	c.Insert("hello")
	c.Move(0, -2)
	c.Newline()
	require.Equal(t, "hel\nlo", c.String())
	require.Equal(t, 1, c.Row)
	require.Equal(t, 0, c.Col)
}

func TestComposer_DeleteWord(t *testing.T) {
	var c Composer
	c.Insert("fix the  flaky")
	c.DeleteWord()
	require.Equal(t, "fix the  ", c.String())
	c.DeleteWord()
	require.Equal(t, "fix ", c.String(), "white space before the word goes with it")
	c.DeleteWord()
	require.Empty(t, c.String())

	// At the start of a line it joins the line above, as backspace does.
	var d Composer
	d.Insert("a\nb")
	d.Home()
	d.DeleteWord()
	require.Equal(t, "ab", d.String())
}

func TestComposer_ClearLine(t *testing.T) {
	var c Composer
	c.Insert("keep\ndrop me")
	c.ClearLine()
	require.Equal(t, "keep\n", c.String())
	require.Equal(t, 1, c.Row)
	require.Equal(t, 0, c.Col)
}

func TestComposer_WideRunes(t *testing.T) {
	var c Composer
	c.Insert("日本")
	c.Backspace()
	require.Equal(t, "日", c.String())
	require.Equal(t, 1, c.Col)

	// Wide runes wrap by cells, not runes: 4 of them need 8 cells.
	var d Composer
	d.Insert("日本語文")
	lines := d.View(6, 3, false, "")
	require.Equal(t, "日本語", lines[0])
	require.Equal(t, "文", lines[1])
	for _, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), 6)
	}
}

func TestComposer_MoveAcrossLines(t *testing.T) {
	var c Composer
	c.Insert("long line\nab")
	c.Move(-1, 0)
	require.Equal(t, 0, c.Row)
	require.Equal(t, 2, c.Col)
	c.End()
	require.Equal(t, 9, c.Col)
	c.Move(1, 0)
	require.Equal(t, 1, c.Row)
	require.Equal(t, 2, c.Col, "the column is clamped to the shorter line")
	c.Move(0, 1) // past the end of the last line: stays
	require.Equal(t, 2, c.Col)
	c.Home()
	c.Move(0, -1) // left at a line start goes to the end of the line above
	require.Equal(t, 0, c.Row)
	require.Equal(t, 9, c.Col)
	c.Move(0, 1)
	require.Equal(t, 1, c.Row)
	require.Equal(t, 0, c.Col)
	c.Move(-5, 0)
	require.Equal(t, 0, c.Row)
}

func TestComposer_ViewWrapsAndWindows(t *testing.T) {
	var c Composer
	for i := range 40 {
		if i > 0 {
			c.Newline()
		}
		c.Insert(fmt.Sprintf("line %d", i))
	}
	c.Move(-20, 0) // row 19
	lines := c.View(20, 4, true, "|")
	require.Len(t, lines, 4)
	require.Contains(t, strings.Join(lines, "\n"), "line 19", "the cursor row stays visible")
	require.Contains(t, strings.Join(lines, "\n"), "|")

	// A long line wraps at the width; the cursor at its end stays visible.
	var d Composer
	d.Insert(strings.Repeat("x", 25))
	lines = d.View(10, 2, true, "|")
	require.Len(t, lines, 2)
	require.Equal(t, "xxxxx|", lines[1], "the window ends on the cursor's row")
	for _, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), 10)
	}

	// Fewer lines than rows: padded to rows.
	var e Composer
	e.Insert("hi")
	require.Equal(t, []string{"hi", "", ""}, e.View(10, 3, false, "|"))

	// Tabs draw as one space so the width stays right.
	var f Composer
	f.Insert("a\tb")
	require.Equal(t, "a\tb", f.String())
	require.Equal(t, "a b", f.View(10, 1, false, "")[0])
}

func TestComposer_ReplaceFirstKeepsCursor(t *testing.T) {
	var c Composer
	c.Insert("ab [p 1]\ncd")
	require.True(t, c.ReplaceFirst("[p 1]", "[log]"))
	require.Equal(t, "ab [log]\ncd", c.String())
	require.Equal(t, 1, c.Row)
	require.Equal(t, 2, c.Col, "a cursor after the match stays on its text")

	c.Row, c.Col = 0, 1
	require.True(t, c.ReplaceFirst("[log]", ""))
	require.Equal(t, "ab \ncd", c.String())
	require.Equal(t, 0, c.Row)
	require.Equal(t, 1, c.Col, "a cursor before the match does not move")

	c.Row, c.Col = 1, 0
	require.True(t, c.ReplaceFirst("ab \nc", "x"))
	require.Equal(t, "xd", c.String())
	require.Equal(t, 0, c.Row)
	require.Equal(t, 1, c.Col, "a cursor inside the match goes to its end")
	require.False(t, c.ReplaceFirst("zz", "y"))
	require.False(t, c.ReplaceFirst("", "y"))
}

func TestComposer_InsertDropsInvisibles(t *testing.T) {
	var c Composer
	c.Insert("a\U000E0041\u200bb")
	require.Equal(t, "ab", c.String())
}
