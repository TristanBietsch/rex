package usage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func fakeFS(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

func none() string { return "" }

func TestFindGitBash(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	t.Run("env var wins", func(t *testing.T) {
		got, err := FindGitBash(env(map[string]string{"CLAUDE_CODE_GIT_BASH_PATH": `D:\Git\bin\bash.exe`}),
			fakeFS(`D:\Git\bin\bash.exe`, `C:\Program Files\Git\bin\bash.exe`), none, none)
		require.NoError(t, err)
		require.Equal(t, `D:\Git\bin\bash.exe`, got)
	})
	t.Run("from git --exec-path", func(t *testing.T) {
		got, err := FindGitBash(env(nil), fakeFS(`C:/Program Files/Git\bin\bash.exe`),
			func() string { return `C:/Program Files/Git/mingw64/libexec/git-core` }, none)
		require.NoError(t, err)
		require.Equal(t, `C:/Program Files/Git\bin\bash.exe`, got)
	})
	t.Run("from where git", func(t *testing.T) {
		got, err := FindGitBash(env(nil), fakeFS(`C:\Program Files\Git\bin\bash.exe`), none,
			func() string { return `C:\Program Files\Git\cmd\git.exe` })
		require.NoError(t, err)
		require.Equal(t, `C:\Program Files\Git\bin\bash.exe`, got)
	})
	t.Run("usr/bin fallback", func(t *testing.T) {
		got, err := FindGitBash(env(nil), fakeFS(`C:\Git\usr\bin\bash.exe`), none, func() string { return `C:\Git\cmd\git.exe` })
		require.NoError(t, err)
		require.Equal(t, `C:\Git\usr\bin\bash.exe`, got)
	})
	t.Run("never System32 (the WSL launcher)", func(t *testing.T) {
		sys := `C:\Windows\System32\bash.exe`
		_, err := FindGitBash(env(map[string]string{"CLAUDE_CODE_GIT_BASH_PATH": sys}), fakeFS(sys), none, none)
		require.ErrorIs(t, err, ErrNoGitBash)
		_, err = FindGitBash(env(nil), fakeFS(`C:\Windows\System32\bin\bash.exe`), none,
			func() string { return `C:\Windows\System32\cmd\git.exe` })
		require.ErrorIs(t, err, ErrNoGitBash)
	})
	t.Run("a missing env path falls through, nothing found is an error", func(t *testing.T) {
		_, err := FindGitBash(env(map[string]string{"CLAUDE_CODE_GIT_BASH_PATH": `Z:\nope\bash.exe`}), fakeFS(), none, none)
		require.ErrorIs(t, err, ErrNoGitBash)
	})
}

func TestUp(t *testing.T) {
	require.Equal(t, `C:\Git`, up(`C:\Git\cmd\git.exe`, 2))
	require.Equal(t, `C:/Git`, up(`C:/Git/mingw64/libexec/git-core/`, 3))
	require.Empty(t, up(`git.exe`, 2))
}

func TestShellFor(t *testing.T) {
	sh, err := shellFor("linux")
	require.NoError(t, err)
	require.Equal(t, []string{"sh", "-c"}, sh)
	sh, err = shellFor("darwin")
	require.NoError(t, err)
	require.Equal(t, []string{"sh", "-c"}, sh)
}
