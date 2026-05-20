package daemonctl

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindPID_NotRunning(t *testing.T) {
	_, err := FindPID()
	// CI may or may not have rex-daemon running; if pgrep fails we expect ErrNotRunning.
	if err != nil {
		require.True(t, errors.Is(err, ErrNotRunning))
	}
}
