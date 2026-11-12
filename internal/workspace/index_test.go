package workspace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func indexWS(t *testing.T) *Workspace {
	t.Helper()
	root := t.TempDir()
	w, err := Init(root, "demo", nil)
	require.NoError(t, err)
	return w
}

func readIndex(t *testing.T, w *Workspace) []IndexLine {
	t.Helper()
	f, err := os.Open(filepath.Join(w.Root, DirName, IndexFileName))
	require.NoError(t, err)
	defer f.Close()
	var out []IndexLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l IndexLine
		require.NoError(t, json.Unmarshal(sc.Bytes(), &l))
		out = append(out, l)
	}
	return out
}

func TestAppendIndex_AppendsLines0600(t *testing.T) {
	w := indexWS(t)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, AppendIndex(w, IndexLine{SessionID: "s1", ShortID: "ab12", Tool: "claude", CWD: w.Root, StartedAt: now, State: "queued"}))
	require.NoError(t, AppendIndex(w, IndexLine{SessionID: "s1", ShortID: "ab12", Tool: "claude", CWD: w.Root, StartedAt: now, EndedAt: now, State: "done", ResumeID: "r"}))
	got := readIndex(t, w)
	require.Len(t, got, 2)
	require.Equal(t, "done", got[1].State)
	require.True(t, got[0].EndedAt.IsZero())
	fi, err := os.Stat(filepath.Join(w.Root, DirName, IndexFileName))
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}
}

func TestAppendIndex_RefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	w := indexWS(t)
	target := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(target, nil, 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(w.Root, DirName, IndexFileName)))
	err := AppendIndex(w, IndexLine{SessionID: "s1", State: "queued"})
	require.ErrorIs(t, err, ErrUnsafe)
	b, _ := os.ReadFile(target)
	require.Empty(t, b)
}

func TestAppendIndex_RefusesUnsafeDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode checks are skipped on Windows")
	}
	w := indexWS(t)
	require.NoError(t, os.Chmod(filepath.Join(w.Root, DirName), 0o777)) //nolint:gosec // the test
	require.ErrorIs(t, AppendIndex(w, IndexLine{SessionID: "s1", State: "queued"}), ErrUnsafe)
}

func TestAppendIndex_NeedsSessionID(t *testing.T) {
	w := indexWS(t)
	require.Error(t, AppendIndex(w, IndexLine{}))
}

func TestAppendIndex_NoRexDirNoCreate(t *testing.T) {
	root := t.TempDir()
	err := AppendIndex(&Workspace{ID: "7f3b89a2", Root: root}, IndexLine{SessionID: "s", State: "queued"})
	require.Error(t, err)
	_, serr := os.Stat(filepath.Join(root, DirName))
	require.True(t, os.IsNotExist(serr), "append never creates .rex/")
}

// A hard link at the index path would make the daemon append into a file
// outside .rex/ (a hostile archive can ship one): refused, target untouched.
func TestAppendIndex_RefusesHardLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("link counts are not checked on Windows")
	}
	w := indexWS(t)
	target := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(target, []byte("keep\n"), 0o600))
	require.NoError(t, os.Link(target, filepath.Join(w.Root, DirName, IndexFileName)))
	err := AppendIndex(w, IndexLine{SessionID: "s1", State: "queued"})
	require.ErrorIs(t, err, ErrUnsafe)
	b, _ := os.ReadFile(target)
	require.Equal(t, "keep\n", string(b))
}

// A directory (or other non-regular file) at the index path is refused.
func TestAppendIndex_RefusesDirectory(t *testing.T) {
	w := indexWS(t)
	require.NoError(t, os.Mkdir(filepath.Join(w.Root, DirName, IndexFileName), 0o700))
	require.ErrorIs(t, AppendIndex(w, IndexLine{SessionID: "s1", State: "queued"}), ErrUnsafe)
}

// A malformed index (last line cut short, no newline) does not swallow the
// next line: it starts on a line of its own.
func TestAppendIndex_MalformedTailKeepsLinesApart(t *testing.T) {
	w := indexWS(t)
	path := filepath.Join(w.Root, DirName, IndexFileName)
	require.NoError(t, os.WriteFile(path, []byte(`{"session_id":"x","sta`), 0o600))
	require.NoError(t, AppendIndex(w, IndexLine{SessionID: "s1", State: "queued"}))
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	require.Len(t, lines, 2)
	var l IndexLine
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &l))
	require.Equal(t, "s1", l.SessionID)
}
