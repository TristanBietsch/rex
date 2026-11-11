package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttachPopupInnerSize_BannerTakesOneRow(t *testing.T) {
	c0, r0 := attachPopupInnerSize(120, 40, 0)
	c1, r1 := attachPopupInnerSize(120, 40, 1)
	require.Equal(t, c0, c1)
	require.Equal(t, r0-1, r1)
	require.Equal(t, 28, r0)
	_, floor := attachPopupInnerSize(120, 10, 1)
	require.Equal(t, 8, floor)
}

func TestResizeAttach_UsesSeamAndBannerRows(t *testing.T) {
	m := attachWithTerm(t, 120, 40, "")
	var got [][2]uint16
	m.Attach.resize = func(c, r uint16) error { got = append(got, [2]uint16{c, r}); return nil }

	resizeAttach(m)
	require.Empty(t, got, "no change, no resize")

	m.Width, m.Height = 130, 41
	resizeAttach(m)
	require.Equal(t, [][2]uint16{{116, 29}}, got)
	require.Equal(t, 29, m.Attach.Rows)

	m.Attach.BannerRows = 1
	m.Width, m.Height = 130, 41
	resizeAttach(m)
	require.Equal(t, [2]uint16{116, 28}, got[len(got)-1])
}
