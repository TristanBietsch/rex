package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassForWidthBreakpoints(t *testing.T) {
	for _, c := range []struct {
		w    int
		want layoutClass
	}{
		{1, classMobile}, {30, classMobile}, {60, classMobile},
		{61, classNarrow}, {109, classNarrow},
		{110, classNormal}, {149, classNormal},
		{150, classWide}, {220, classWide},
	} {
		require.Equal(t, c.want, classForWidth(c.w), "width %d", c.w)
	}
}
