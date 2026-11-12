package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func baseTool() Tool {
	return Tool{ID: "x", Models: []Model{{ID: "m"}}, Detect: Detect{Kind: "heuristic", PromptRegex: "p", IdleMs: 100}}
}

func TestLoaderModelsCmdValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		mc   ModelsCmd
		err  string // "" = valid
	}{
		{"lines ok", ModelsCmd{Args: []string{"models"}, Parse: "lines"}, ""},
		{"parse defaults to lines", ModelsCmd{Args: []string{"models"}}, ""},
		{"json ok", ModelsCmd{Args: []string{"models", "--json"}, Parse: "json", JSONPath: "models[].id"}, ""},
		{"regex ok", ModelsCmd{Args: []string{"m"}, IDRegex: `^(\S+) - `, SkipHeader: 2, TimeoutMs: 5000}, ""},
		{"no args", ModelsCmd{}, "models_cmd.args"},
		{"empty arg", ModelsCmd{Args: []string{"m", ""}}, "models_cmd.args"},
		{"bad parse", ModelsCmd{Args: []string{"m"}, Parse: "xml"}, "models_cmd.parse"},
		{"json without path", ModelsCmd{Args: []string{"m"}, Parse: "json"}, "json_path"},
		{"json_path on lines", ModelsCmd{Args: []string{"m"}, JSONPath: "a[].b"}, "json_path only applies"},
		{"bad json_path", ModelsCmd{Args: []string{"m"}, Parse: "json", JSONPath: "a..b"}, "json_path"},
		{"regex on json", ModelsCmd{Args: []string{"m"}, Parse: "json", JSONPath: "a[].b", IDRegex: `(x)`}, "id_regex only applies"},
		{"bad regex", ModelsCmd{Args: []string{"m"}, IDRegex: `(`}, "id_regex"},
		{"regex without group", ModelsCmd{Args: []string{"m"}, IDRegex: `^\S+`}, "capture group"},
		{"negative skip", ModelsCmd{Args: []string{"m"}, SkipHeader: -1}, "skip_header"},
		{"huge skip", ModelsCmd{Args: []string{"m"}, SkipHeader: 100}, "skip_header"},
		{"timeout too low", ModelsCmd{Args: []string{"m"}, TimeoutMs: 99}, "timeout_ms"},
		{"timeout too high", ModelsCmd{Args: []string{"m"}, TimeoutMs: 30001}, "timeout_ms"},
		{"timeout min", ModelsCmd{Args: []string{"m"}, TimeoutMs: 100}, ""},
		{"timeout max", ModelsCmd{Args: []string{"m"}, TimeoutMs: 30000}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := baseTool()
			mc := tc.mc
			tool.ModelsCmd = &mc
			err := validate([]Tool{tool})
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestValidateModelArgTemplate(t *testing.T) {
	tool := baseTool()
	tool.ModelArgTemplate = []string{"--model", "{model}"}
	require.NoError(t, validate([]Tool{tool}))
	tool.ModelArgTemplate = []string{"--model=x"}
	require.ErrorContains(t, validate([]Tool{tool}), "{model}")
	tool.ModelArgTemplate = []string{"{model}", ""}
	require.ErrorContains(t, validate([]Tool{tool}), "model_arg_template")
}

func TestValidatePromptArgs(t *testing.T) {
	tool := baseTool()
	for _, ok := range [][]string{{"{prompt}"}, {"--", "{prompt}"}, {"-i", "{prompt}"}, {"--prompt-interactive={prompt}"}} {
		tool.PromptArgs = ok
		require.NoError(t, validate([]Tool{tool}), ok)
	}
	for name, bad := range map[string][]string{
		"no placeholder":  {"-i", "text"},
		"twice":           {"{prompt}", "{prompt}"},
		"twice in one":    {"{prompt}{prompt}"},
		"empty element":   {"", "{prompt}"},
		"only separators": {"--"},
	} {
		tool.PromptArgs = bad
		require.ErrorContains(t, validate([]Tool{tool}), "prompt_args", name)
	}
	tool.PromptArgs = []string{"{prompt}"}
	tool.PromptPaste = &PromptPaste{Plain: true}
	require.ErrorContains(t, validate([]Tool{tool}), "prompt_paste")
	tool.PromptArgs = nil
	tool.PromptPaste = &PromptPaste{Plain: true, SettleMs: 800}
	require.NoError(t, validate([]Tool{tool}))
	tool.PromptPaste = &PromptPaste{SettleMs: 20000}
	require.ErrorContains(t, validate([]Tool{tool}), "settle_ms")
}

func TestPromptArgsFor(t *testing.T) {
	tool := Tool{PromptArgs: []string{"-i", "{prompt}"}}
	require.Equal(t, []string{"-i", "a b {prompt} -x"}, tool.PromptArgsFor("a b {prompt} -x"), "one argv element, no re-expansion")
	require.Equal(t, []string{"--prompt-interactive=hi"}, Tool{PromptArgs: []string{"--prompt-interactive={prompt}"}}.PromptArgsFor("hi"))
	require.Nil(t, Tool{}.PromptArgsFor("hi"))
}

func TestModelArgsFor(t *testing.T) {
	tool := Tool{ModelArgTemplate: []string{"--model", "{model}"}}
	require.Equal(t, []string{"--model", "abc-1"}, tool.ModelArgsFor("abc-1"))
	require.Nil(t, Tool{}.ModelArgsFor("abc"))
}

func TestValidateLoginRegex(t *testing.T) {
	tool := baseTool()
	tool.LoginRegex = "Press any key to log in"
	require.NoError(t, validate([]Tool{tool}))
	tool.LoginRegex = "("
	require.ErrorContains(t, validate([]Tool{tool}), "login_regex")
}

func TestMergeNewFields(t *testing.T) {
	base := Tool{ID: "a", PromptArgs: []string{"{prompt}"}, ModelArgTemplate: []string{"--model", "{model}"},
		LoginRegex: "old", ModelsCmd: &ModelsCmd{Args: []string{"x"}}}
	over := Tool{ID: "a", PromptArgs: []string{"-i", "{prompt}"}, ModelArgTemplate: []string{"-m", "{model}"},
		LoginRegex: "new", ModelsCmd: &ModelsCmd{Args: []string{"y"}}}
	m := mergeOne(base, over)
	require.Equal(t, over.PromptArgs, m.PromptArgs)
	require.Equal(t, over.ModelArgTemplate, m.ModelArgTemplate)
	require.Equal(t, "new", m.LoginRegex)
	require.Equal(t, []string{"y"}, m.ModelsCmd.Args)
	kept := mergeOne(base, Tool{ID: "a", Name: "n"})
	require.Equal(t, base.PromptArgs, kept.PromptArgs)
	require.Equal(t, "old", kept.LoginRegex)
}

// Existing tools keep their prompt delivery after the tool-id switch moved
// into the registry.
func TestBuiltinPromptDelivery(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	want := map[string][]string{
		"claude":      {"--", "{prompt}"},
		"codex":       {"--", "{prompt}"},
		"antigravity": {"--prompt-interactive={prompt}"},
		"gemini":      {"--prompt-interactive={prompt}"},
	}
	for id, args := range want {
		tool, ok := reg.Find(id)
		require.True(t, ok, id)
		require.Equal(t, args, tool.PromptArgs, id)
	}
	ollama, _ := reg.Find("ollama")
	require.Empty(t, ollama.PromptArgs)
	require.Equal(t, &PromptPaste{Plain: true, SettleMs: 800}, ollama.PromptPaste)
	echo, _ := reg.Find("echo")
	require.Empty(t, echo.PromptArgs)
	require.Nil(t, echo.PromptPaste)
}
