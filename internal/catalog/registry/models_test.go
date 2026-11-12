package registry

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Synthetic outputs shaped like the captured model lists (never real bytes).
const (
	qwenModels = "MODEL\nQwen3.8-Flash (qfmodel)\nQwen3.8-Coder (qcmodel)\n"
	grokModels = "Available models:\n  * grok-4.7 (default)\n  * grok-4.7-fast\n"
	curModels  = "Available models\n\nauto - Auto (current, default)\ngpt-5.3-codex-high - Codex 5.3 High\nclaude-opus-5-thinking-high - Claude Opus 5 1M Thinking\n"
)

func builtinTool(t *testing.T, id string) Tool {
	t.Helper()
	reg, err := Load("")
	require.NoError(t, err)
	tool, ok := reg.Find(id)
	require.True(t, ok, id)
	return tool
}

func ids(models []Model) []string {
	out := make([]string, len(models))
	for i, m := range models {
		out[i] = m.ID
	}
	return out
}

func TestParseModelIDs_BuiltinFormats(t *testing.T) {
	for _, tc := range []struct {
		tool, out string
		want      []string
	}{
		{"qwen", qwenModels, []string{"qfmodel", "qcmodel"}},
		{"grok", grokModels, []string{"grok-4.7", "grok-4.7-fast"}},
		{"cursor", curModels, []string{"auto", "gpt-5.3-codex-high", "claude-opus-5-thinking-high"}},
		{"qwen", strings.ReplaceAll(qwenModels, "\n", "\r\n"), []string{"qfmodel", "qcmodel"}},
	} {
		got, err := ParseModelIDs(*builtinTool(t, tc.tool).ModelsCmd, []byte(tc.out))
		require.NoError(t, err, tc.tool)
		require.Equal(t, tc.want, got, tc.tool)
	}
}

func TestParseModelIDs_JSONAndPlainLines(t *testing.T) {
	js := ModelsCmd{Args: []string{"x"}, Parse: ParseJSON, JSONPath: "models[].id"}
	got, err := ParseModelIDs(js, []byte(`{"models":[{"id":"a-1"},{"id":"b.2"},{"name":"nope"},{"id":"a-1"},{"id":"bad id"}]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"a-1", "b.2"}, got, "duplicates, missing and invalid ids dropped")

	got, err = ParseModelIDs(ModelsCmd{Args: []string{"x"}, Parse: ParseJSON, JSONPath: "[].id"}, []byte(`[{"id":"x"},{"id":"y"}]`))
	require.NoError(t, err)
	require.Equal(t, []string{"x", "y"}, got)

	_, err = ParseModelIDs(js, []byte(`{"models": "nope"}`))
	require.ErrorIs(t, err, ErrNoModels)
	_, err = ParseModelIDs(js, []byte(`not json`))
	require.Error(t, err)

	// No id_regex: each trimmed line is the id; header lines are skipped.
	got, err = ParseModelIDs(ModelsCmd{Args: []string{"x"}, SkipHeader: 1}, []byte("HEADER\n  m1 \n\nm2\n"))
	require.NoError(t, err)
	require.Equal(t, []string{"m1", "m2"}, got)
	_, err = ParseModelIDs(ModelsCmd{Args: []string{"x"}, SkipHeader: 5}, []byte("only\n"))
	require.ErrorIs(t, err, ErrNoModels)
}

// An auth error on stdout must parse to nothing, never to a model named
// "Please".
func TestParseModelIDs_AuthErrorAndGarbageYieldNothing(t *testing.T) {
	for _, tool := range []string{"qwen", "grok", "cursor"} {
		mc := *builtinTool(t, tool).ModelsCmd
		for _, out := range []string{
			"Error: not logged in. Please log in with `" + tool + " login`.\n",
			"\x00\x01\x02 garbage \xff\xfe\n",
			"",
			strings.Repeat("= = =\n", 50),
		} {
			_, err := ParseModelIDs(mc, []byte(out))
			require.ErrorIs(t, err, ErrNoModels, "%s: %q", tool, out)
		}
	}
	// Without an id_regex the whole line would be the id; a sentence has
	// spaces, so the validator rejects it all the same.
	_, err := ParseModelIDs(ModelsCmd{Args: []string{"x"}}, []byte("Please log in first\n"))
	require.ErrorIs(t, err, ErrNoModels)
}

func TestValidModelID(t *testing.T) {
	for id, want := range map[string]bool{
		"auto":                         true,
		"gpt-5.3-codex-high":           true,
		"claude-opus-5-thinking-1m":    true,
		"org/model:tag@v1":             true,
		"a[1]":                         true,
		strings.Repeat("a", 128):       true,
		strings.Repeat("a", 129):       false,
		"":                             false,
		"has space":                    false,
		"semi;colon":                   false,
		"$(whoami)":                    false,
		"back`tick`":                   false,
		"quote\"d":                     false,
		"pipe|x":                       false,
		"new\nline":                    false,
		"tab\tx":                       false,
		"unicodé":                      false,
		"--flag":                       false,
		"-m":                           false,
		"a&b":                          false,
		"a>b":                          false,
		"a\\b":                         false,
		"'quoted'":                     false,
		"trailing\x00":                 false,
		"Qwen3.8-Flash (qfmodel)":      false,
		"-":                            false,
		"x-":                           true,
		strings.Repeat("a", 127) + "-": true,
	} {
		require.Equal(t, want, ValidModelID(id), "%q", id)
	}
}

// ---- stub binaries ----

func stubTool(t *testing.T, dir, name, body string, mc ModelsCmd) Tool {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("model-list stubs are POSIX shell scripts")
	}
	stubBinary(t, dir, name, body)
	// A fresh script can take seconds to run the first time on a loaded
	// machine (the OS scans it); keep the default timeout out of the way.
	if mc.TimeoutMs == 0 {
		mc.TimeoutMs = 25000
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return Tool{ID: name, Command: []string{name}, ModelsCmd: &mc, ModelArgTemplate: []string{"--model", "{model}"},
		ModelEffort: &Effort{Options: []string{"low", "high"}, Default: "low", ArgTemplate: "--e={value}"}}
}

func TestDetectModels_Lines(t *testing.T) {
	dir := t.TempDir()
	tool := stubTool(t, dir, "stub-lines", `printf 'MODEL\nOne (m-one)\nTwo (m-two)\n'`,
		ModelsCmd{Args: []string{"--list-models"}, IDRegex: `\(([^)]+)\)\s*$`, SkipHeader: 1})
	models, err := DetectModels(context.Background(), tool)
	require.NoError(t, err)
	require.Equal(t, []string{"m-one", "m-two"}, ids(models))
	require.Equal(t, []string{"--model", "m-one"}, models[0].Args)
	require.Equal(t, "m-one", models[0].Name)
	require.True(t, models[0].Detected)
	require.Equal(t, tool.ModelEffort, models[0].Effort)
}

func TestDetectModels_JSON(t *testing.T) {
	dir := t.TempDir()
	tool := stubTool(t, dir, "stub-json", `printf '{"models":[{"id":"j1"},{"id":"j2"}]}'`,
		ModelsCmd{Args: []string{"list"}, Parse: ParseJSON, JSONPath: "models[].id"})
	models, err := DetectModels(context.Background(), tool)
	require.NoError(t, err)
	require.Equal(t, []string{"j1", "j2"}, ids(models))
}

// The command's own args follow Command; stdin is empty.
func TestDetectModels_ArgsAndNoStdin(t *testing.T) {
	dir := t.TempDir()
	tool := stubTool(t, dir, "stub-args", `echo "$1"; cat | wc -c | tr -d ' '`, ModelsCmd{Args: []string{"models"}})
	models, err := DetectModels(context.Background(), tool)
	require.NoError(t, err)
	require.Equal(t, []string{"models", "0"}, ids(models), "first arg is models_cmd.args, stdin is empty")
}

func TestDetectModels_FailuresFallBack(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		body string
		mc   ModelsCmd
		want string
	}{
		"garbage":    {`printf '\001\002 not a list \377\n'`, ModelsCmd{Args: []string{"m"}}, "no models"},
		"auth error": {`echo "Error: please log in"; exit 0`, ModelsCmd{Args: []string{"m"}, IDRegex: `^(\S+) - `}, "no models"},
		"nonzero":    {`echo "m1 - x"; exit 3`, ModelsCmd{Args: []string{"m"}, IDRegex: `^(\S+) - `}, "run"},
		"timeout":    {`sleep 5; echo "m1 - x"`, ModelsCmd{Args: []string{"m"}, IDRegex: `^(\S+) - `, TimeoutMs: 150}, "timed out"},
		"1 MiB":      {`head -c 1048576 /dev/zero | tr '\0' 'a'`, ModelsCmd{Args: []string{"m"}}, "output over"},
		"just over":  {`head -c 65537 /dev/zero | tr '\0' 'a'`, ModelsCmd{Args: []string{"m"}}, "output over"},
	} {
		t.Run(name, func(t *testing.T) {
			tool := stubTool(t, dir, "stub-"+strings.ReplaceAll(name, " ", "-"), tc.body, tc.mc)
			start := time.Now()
			models, err := DetectModels(context.Background(), tool)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			require.Empty(t, models)
			require.Less(t, time.Since(start), 4*time.Second, "bounded by the timeout, not the script")
		})
	}
}

func TestDetectModels_Errors(t *testing.T) {
	_, err := DetectModels(context.Background(), Tool{ID: "x", Command: []string{"definitely-not-installed-xyz"},
		ModelsCmd: &ModelsCmd{Args: []string{"m"}}, ModelArgTemplate: []string{"{model}"}})
	require.ErrorContains(t, err, "binary not found")
	_, err = DetectModels(context.Background(), Tool{ID: "x", Command: []string{"x"}, ModelsCmd: &ModelsCmd{Args: []string{"m"}}})
	require.ErrorContains(t, err, "model_arg_template")
}

func TestMergeModels(t *testing.T) {
	static := []Model{{ID: "default", Name: "Default model", Effort: &Effort{Default: "low"}}, {ID: "b", Name: "B static", Args: []string{"--b"}}}
	detected := []Model{{ID: "a", Name: "a", Detected: true}, {ID: "b", Name: "b", Detected: true}, {ID: "a", Name: "a", Detected: true}}
	got := MergeModels(static, detected)
	require.Equal(t, []string{"a", "b", "default"}, ids(got), "detected first, then static ones it did not list")
	require.True(t, got[1].Detected)
	require.Equal(t, "B static", got[1].Name, "a static definition wins for the same id")
	require.Equal(t, []string{"--b"}, got[1].Args)
	require.False(t, got[2].Detected)

	require.Equal(t, static, MergeModels(static, nil), "nothing detected: static list")
}

func TestRegistryWithModelsAndWithout(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	qwen, _ := reg.Find("qwen")
	merged := reg.WithModels(map[string][]Model{
		"qwen":    {{ID: "qfmodel", Name: "qfmodel", Args: qwen.ModelArgsFor("qfmodel"), Detected: true}},
		"unknown": {{ID: "z"}},
	})
	_, m, ok := merged.FindModel("qwen", "qfmodel")
	require.True(t, ok)
	require.Equal(t, []string{"--model", "qfmodel"}, m.Args)
	_, _, ok = reg.FindModel("qwen", "qfmodel")
	require.False(t, ok, "the receiver is unchanged")
	require.Len(t, merged.Tools, len(reg.Tools))

	off := reg.Without(ExtraAgentIDs...)
	for _, id := range ExtraAgentIDs {
		_, ok := off.Find(id)
		require.False(t, ok, id)
	}
	_, ok = off.Find("claude")
	require.True(t, ok)
	_, ok = reg.Find("qwen")
	require.True(t, ok, "the receiver is unchanged")
}

// ---- cache ----

func TestRefreshModelsCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(t.TempDir(), "state", "models-cache.json")
	tool := stubTool(t, dir, "stub-cache", `printf 'm1 - One\nm2 - Two\n'`, ModelsCmd{Args: []string{"m"}, IDRegex: `^(\S+) - `})
	tools := []Tool{tool}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

	require.Empty(t, CachedModels(tools, cache, now, false), "no file yet")
	got := RefreshModels(context.Background(), tools, cache, false, now)
	require.Equal(t, []string{"m1", "m2"}, ids(got["stub-cache"]))
	fi, err := os.Stat(cache)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}

	require.Equal(t, []string{"m1", "m2"}, ids(CachedModels(tools, cache, now.Add(time.Hour), false)["stub-cache"]))

	// Rewriting the binary changes its mtime, which invalidates the entry.
	time.Sleep(20 * time.Millisecond)
	stubBinary(t, dir, "stub-cache", `printf 'm9 - Nine\n'`)
	got = RefreshModels(context.Background(), tools, cache, false, now.Add(time.Hour))
	require.Equal(t, []string{"m9"}, ids(got["stub-cache"]), "a new binary mtime invalidates the entry")
}

func TestRefreshModelsCacheServesAndExpires(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(t.TempDir(), "models-cache.json")
	tool := stubTool(t, dir, "stub-age", `printf 'm1 - One\n'`, ModelsCmd{Args: []string{"m"}, IDRegex: `^(\S+) - `})
	tools := []Tool{tool}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	RefreshModels(context.Background(), tools, cache, false, now)

	// Poison the cached ids: a valid entry answers without running the stub.
	raw, err := os.ReadFile(cache)
	require.NoError(t, err)
	poisoned := strings.Replace(string(raw), `"m1"`, `"cached-id","bad id","-x"`, 1)
	require.NoError(t, os.WriteFile(cache, []byte(poisoned), 0o600)) //nolint:gosec // test path under t.TempDir()
	got := RefreshModels(context.Background(), tools, cache, false, now.Add(23*time.Hour))
	require.Equal(t, []string{"cached-id"}, ids(got["stub-age"]), "served from the cache, ids re-validated")

	// 24 h later the entry is too old: the command runs again.
	got = RefreshModels(context.Background(), tools, cache, false, now.Add(25*time.Hour))
	require.Equal(t, []string{"m1"}, ids(got["stub-age"]))

	// force ignores a fresh entry.
	require.NoError(t, os.WriteFile(cache, []byte(poisoned), 0o600)) //nolint:gosec // test path under t.TempDir()
	got = RefreshModels(context.Background(), tools, cache, true, now.Add(time.Minute))
	require.Equal(t, []string{"m1"}, ids(got["stub-age"]))

	// Stale entries can still be read for display.
	require.Empty(t, CachedModels(tools, cache, now.Add(48*time.Hour), false))
	require.NotEmpty(t, CachedModels(tools, cache, now.Add(48*time.Hour), true))
}

func TestRefreshModelsKeyedByBinaryMtime(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(t.TempDir(), "models-cache.json")
	tool := stubTool(t, dir, "stub-mtime", `printf 'm1 - One\n'`, ModelsCmd{Args: []string{"m"}, IDRegex: `^(\S+) - `})
	tools := []Tool{tool}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	RefreshModels(context.Background(), tools, cache, false, now)
	require.NotEmpty(t, CachedModels(tools, cache, now, false))

	bin := filepath.Join(dir, "stub-mtime")
	require.NoError(t, os.Chtimes(bin, now, now.Add(-72*time.Hour)))
	require.Empty(t, CachedModels(tools, cache, now, false), "binary mtime changed")
	require.Empty(t, CachedModels(tools, cache, now, true), "even a stale read needs the same binary")

	// A different command invalidates too.
	tool2 := tool
	mc := *tool.ModelsCmd
	mc.Args = []string{"other"}
	tool2.ModelsCmd = &mc
	RefreshModels(context.Background(), tools, cache, false, now)
	require.Empty(t, CachedModels([]Tool{tool2}, cache, now, false))
}

// A failed detection logs once, leaves the tool out (static list) and drops a
// stale entry.
func TestRefreshModelsFailureFallsBack(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(t.TempDir(), "models-cache.json")
	tool := stubTool(t, dir, "stub-fail", `echo "Please log in"; exit 1`, ModelsCmd{Args: []string{"m"}})
	now := time.Now()
	got := RefreshModels(context.Background(), []Tool{tool}, cache, false, now)
	require.Empty(t, got)
	_, err := os.Stat(cache)
	require.True(t, os.IsNotExist(err), "nothing worth caching, nothing written")

	// A tool without a binary, or without models_cmd, is not even tried.
	missing := Tool{ID: "gone", Command: []string{"not-installed-xyz"}, ModelsCmd: &ModelsCmd{Args: []string{"m"}}, ModelArgTemplate: []string{"{model}"}}
	plain := Tool{ID: "plain", Command: []string{"sh"}}
	require.Empty(t, RefreshModels(context.Background(), []Tool{missing, plain}, cache, true, now))
}

func TestCorruptCacheIgnored(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "models-cache.json")
	require.NoError(t, os.WriteFile(cache, []byte("{not json"), 0o600))
	require.Empty(t, CachedModels([]Tool{{ID: "x"}}, cache, time.Now(), true))
	require.Empty(t, CachedModels([]Tool{{ID: "x"}}, "", time.Now(), true))
}
