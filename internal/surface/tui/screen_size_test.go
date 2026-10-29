package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestScreenSize_DefaultsAgreeBeforeFirstResize(t *testing.T) {
	m := hostileModel(0, 0)
	for i := 5; i <= 30; i++ {
		m.Sessions = append(m.Sessions, protocol.SessionSummary{ID: fmt.Sprint(i), ShortID: fmt.Sprintf("s%03d", i),
			Slug: fmt.Sprintf("slug-%d", i), State: protocol.StateWorking})
	}
	w, h := screenSize(m)
	require.Equal(t, [2]int{100, 32}, [2]int{w, h})
	require.Len(t, strings.Split(m.View(), "\n"), h)
	require.Equal(t, h-boardTop(m)-boardBottomRows, boardHeight(m))
	requireHits(t, m, "default size")
}
