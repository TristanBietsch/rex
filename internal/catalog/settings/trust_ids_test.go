package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTrustSetting(t *testing.T) {
	require.True(t, IsTrustSetting("skip_permissions"))
	require.True(t, IsTrustSetting("auto_trust_dirs"))
	require.False(t, IsTrustSetting("usage"))
	for _, id := range TrustSettingIDs {
		_, ok := Find(id)
		require.True(t, ok, id)
	}
}
