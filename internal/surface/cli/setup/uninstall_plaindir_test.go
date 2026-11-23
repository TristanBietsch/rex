package setup

import (
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// modeInfo is an fs.FileInfo that reports only a mode.
type modeInfo struct{ mode fs.FileMode }

func (m modeInfo) Name() string       { return "x" }
func (m modeInfo) Size() int64        { return 0 }
func (m modeInfo) Mode() fs.FileMode  { return m.mode }
func (m modeInfo) ModTime() time.Time { return time.Time{} }
func (m modeInfo) IsDir() bool        { return m.mode.IsDir() }
func (m modeInfo) Sys() any           { return nil }

// TestPlainDir pins that a Windows junction (dir plus irregular bits) and a
// symlink are not plain directories.
func TestPlainDir(t *testing.T) {
	require.True(t, plainDir(modeInfo{fs.ModeDir}))
	require.False(t, plainDir(modeInfo{fs.ModeDir | fs.ModeIrregular}))
	require.False(t, plainDir(modeInfo{fs.ModeDir | fs.ModeSymlink}))
	require.False(t, plainDir(modeInfo{0}))
}
