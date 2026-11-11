package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStage_CopiesSelected(t *testing.T) {
	lib := t.TempDir()
	writeSkill(t, lib, "review", "---\ndescription: r\n---\nbody")
	require.NoError(t, os.MkdirAll(filepath.Join(lib, "review", "refs"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(lib, "review", "refs", "a.md"), []byte("ref"), 0o600))
	writeSkill(t, lib, "other", "other body")
	session := t.TempDir()

	plugin, err := Stage(lib, session, []string{"review"})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(session, "plugin"), plugin)

	raw, err := os.ReadFile(filepath.Join(plugin, ".claude-plugin", "plugin.json"))
	require.NoError(t, err)
	var m map[string]string
	require.NoError(t, json.Unmarshal(raw, &m))
	require.Equal(t, "rex", m["name"])
	require.Equal(t, "1", m["version"])

	body, err := os.ReadFile(filepath.Join(plugin, "skills", "review", "SKILL.md"))
	require.NoError(t, err)
	require.Contains(t, string(body), "body")
	ref, err := os.ReadFile(filepath.Join(plugin, "skills", "review", "refs", "a.md"))
	require.NoError(t, err)
	require.Equal(t, "ref", string(ref))
	_, err = os.Stat(filepath.Join(plugin, "skills", "other"))
	require.True(t, os.IsNotExist(err), "unselected skills are not copied")

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(plugin)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
	}

	// Restaging replaces the folder (resume picks up library edits).
	writeSkill(t, lib, "review", "new body")
	_, err = Stage(lib, session, []string{"review"})
	require.NoError(t, err)
	body, _ = os.ReadFile(filepath.Join(plugin, "skills", "review", "SKILL.md"))
	require.Equal(t, "new body", string(body))
}

func TestStage_RejectsBadNames(t *testing.T) {
	lib := t.TempDir()
	writeSkill(t, lib, "ok", "x")
	for _, bad := range []string{"../x", "a/b", "", "..", "A", `a\b`} {
		_, err := Stage(lib, t.TempDir(), []string{bad})
		require.Error(t, err, bad)
	}
	_, err := Stage(lib, t.TempDir(), []string{"ok", "ok"})
	require.ErrorContains(t, err, "duplicate")
	_, err = Stage(lib, t.TempDir(), nil)
	require.Error(t, err)
	many := make([]string, MaxPerSession+1)
	for i := range many {
		many[i] = "s" + strconv.Itoa(i)
	}
	_, err = Stage(lib, t.TempDir(), many)
	require.ErrorContains(t, err, "too many")
}

func TestStage_UnknownSkill(t *testing.T) {
	session := t.TempDir()
	_, err := Stage(t.TempDir(), session, []string{"missing"})
	require.ErrorContains(t, err, "not found")
	_, statErr := os.Stat(filepath.Join(session, "plugin"))
	require.True(t, os.IsNotExist(statErr), "nothing is created on failure")
}

func TestStage_RefusesSymlinkInsideSkill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	lib := t.TempDir()
	writeSkill(t, lib, "evil", "x")
	secret := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(secret, []byte("s3cret"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(lib, "evil", "leak.md")))
	session := t.TempDir()
	_, err := Stage(lib, session, []string{"evil"})
	require.ErrorContains(t, err, "symlink")
	_, statErr := os.Stat(filepath.Join(session, "plugin"))
	require.True(t, os.IsNotExist(statErr), "a failed stage is cleaned up")
}

func TestStage_RefusesSymlinkedSkillFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	lib := t.TempDir()
	outside := t.TempDir()
	writeSkill(t, outside, "real", "x")
	require.NoError(t, os.Symlink(filepath.Join(outside, "real"), filepath.Join(lib, "real")))
	_, err := Stage(lib, t.TempDir(), []string{"real"})
	require.Error(t, err)
}

func TestStage_SizeCap(t *testing.T) {
	lib := t.TempDir()
	writeSkill(t, lib, "big", "x")
	require.NoError(t, os.WriteFile(filepath.Join(lib, "big", "blob"), make([]byte, MaxBytes+1), 0o600))
	_, err := Stage(lib, t.TempDir(), []string{"big"})
	require.ErrorContains(t, err, "too large")
}

func TestStage_FileCountCap(t *testing.T) {
	lib := t.TempDir()
	writeSkill(t, lib, "many", "x")
	for i := 0; i < MaxFiles; i++ { // SKILL.md plus these is MaxFiles+1
		require.NoError(t, os.WriteFile(filepath.Join(lib, "many", "f"+strconv.Itoa(i)), []byte("x"), 0o600))
	}
	_, err := Stage(lib, t.TempDir(), []string{"many"})
	require.ErrorContains(t, err, "too large")
	require.True(t, strings.Contains(err.Error(), "files"))
}
