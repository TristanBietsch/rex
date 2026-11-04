package daemonctl

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// FindPID asks the host for real processes, so the result depends on what
// runs there; only the error contract is fixed. It never signals anything.
func TestFindPID_ErrorContract(t *testing.T) {
	pid, err := FindPID()
	if err != nil {
		require.True(t, errors.Is(err, ErrNotRunning) || errors.Is(err, ErrMultipleDaemons), "unexpected error %v", err)
		return
	}
	require.NotEmpty(t, pid)
}
