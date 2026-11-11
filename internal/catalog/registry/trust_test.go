package registry

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// Screen text as the daemon's emulator renders Claude Code's dialog.
const (
	claudeTrustNo = `Accessing workspace:
 /home/u/dev/project
 Quick safety check: Is this a project you created or one you trust?
 ❯ No, exit
   Yes, I trust this folder
 Enter to confirm · Esc to cancel`
	claudeTrustYes = `Accessing workspace:
 /home/u/dev/project
   No, exit
 ❯ Yes, I trust this folder
 Enter to confirm · Esc to cancel`
	// Older Claude Code wording.
	claudeTrustOld = ` Do you trust the files in this folder?
 ❯ 1. Yes, proceed
   2. No, exit`
)

func TestBuiltinClaudeTrustPrompt(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	claude, ok := reg.Find("claude")
	require.True(t, ok)
	tp := claude.TrustPrompt
	require.NotNil(t, tp)
	shown, selected := regexp.MustCompile(tp.Shown), regexp.MustCompile(tp.Selected)

	require.True(t, shown.MatchString(claudeTrustNo))
	require.False(t, selected.MatchString(claudeTrustNo), "\"No, exit\" is selected")
	require.True(t, shown.MatchString(claudeTrustYes))
	require.True(t, selected.MatchString(claudeTrustYes))
	require.True(t, shown.MatchString(claudeTrustOld))
	require.True(t, selected.MatchString(claudeTrustOld))
	require.False(t, shown.MatchString("❯ Try \"fix the tests\"\n? for shortcuts"), "the normal prompt is not the dialog")
	require.False(t, shown.MatchString("⏺ The docs say to pick \"❯ Yes, I trust this folder\""), "quoting the option without the dialog header")
	require.False(t, selected.MatchString("Accessing workspace:\n ❯\n   Yes, I trust this folder"), "selected must not span lines")

	require.Equal(t, "\x1b[B", tp.Move)
	require.Equal(t, "\r", tp.Confirm)
}

func TestValidateTrustPrompt(t *testing.T) {
	base := Tool{ID: "x", Detect: Detect{Kind: "hooks", Format: "claude"}, Models: []Model{{ID: "m"}}}
	ok := base
	ok.TrustPrompt = &TrustPrompt{Shown: "a", Selected: "b", Move: "\x1b[B", Confirm: "\r"}
	require.NoError(t, validate([]Tool{ok}))

	missing := base
	missing.TrustPrompt = &TrustPrompt{Shown: "a", Selected: "b", Move: "\x1b[B"}
	require.ErrorContains(t, validate([]Tool{missing}), "trust_prompt needs")

	badRegex := base
	badRegex.TrustPrompt = &TrustPrompt{Shown: "(", Selected: "b", Move: "\x1b[B", Confirm: "\r"}
	require.ErrorContains(t, validate([]Tool{badRegex}), "trust_prompt regex")
}

func TestMergeTrustPrompt(t *testing.T) {
	user := &TrustPrompt{Shown: "s", Selected: "t", Move: "\x1b[A", Confirm: "\r"}
	merged := mergeOne(Tool{ID: "claude", TrustPrompt: &TrustPrompt{Shown: "old"}}, Tool{ID: "claude", TrustPrompt: user})
	require.Equal(t, user, merged.TrustPrompt)
	kept := mergeOne(Tool{ID: "claude", TrustPrompt: user}, Tool{ID: "claude", Name: "renamed"})
	require.Equal(t, user, kept.TrustPrompt, "an overlay without trust_prompt keeps the builtin one")
}

func TestBuiltinAntigravity(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	agy, ok := reg.Find("antigravity")
	require.True(t, ok)
	require.Equal(t, []string{"agy", "--dangerously-skip-permissions"}, agy.Command)
	require.Nil(t, agy.TrustPrompt, "agy trusts the folder from --settings; no keystrokes")
	require.Equal(t, []string{"--settings", `{"trustedWorkspaces":{cwd_list}}`}, agy.TrustArgs)
	gemini, ok := reg.Find("gemini")
	require.True(t, ok, "kept so partial overlays and saved defaults still work")
	require.NotNil(t, gemini.EnabledByDefault)
	require.False(t, *gemini.EnabledByDefault, "retired: hidden from the wizard")
	require.Equal(t, `\? for shortcuts`, agy.Detect.PromptRegex)
	require.Equal(t, "esc to cancel", agy.Detect.BusyRegex)

	_, flash, ok := reg.FindModel("antigravity", "gemini-3.8-flash")
	require.True(t, ok)
	require.Equal(t, "--model=gemini-3.8-flash-{value}", flash.Effort.ArgTemplate, "effort picks the slug")
}

func TestValidateTrustArgs(t *testing.T) {
	base := Tool{ID: "x", Detect: Detect{Kind: "hooks", Format: "claude"}, Models: []Model{{ID: "m"}}}
	ok := base
	ok.TrustArgs = []string{"--settings", `{"trustedWorkspaces":{cwd_list}}`}
	require.NoError(t, validate([]Tool{ok}))

	noFolder := base
	noFolder.TrustArgs = []string{"--trust-everything"}
	require.ErrorContains(t, validate([]Tool{noFolder}), "must name the folder")
}

func TestMergeTrustArgs(t *testing.T) {
	merged := mergeOne(Tool{ID: "a", TrustArgs: []string{"--old", "{cwd_list}"}}, Tool{ID: "a", TrustArgs: []string{"--new", "{cwd_list}"}})
	require.Equal(t, []string{"--new", "{cwd_list}"}, merged.TrustArgs)
	kept := mergeOne(Tool{ID: "a", TrustArgs: []string{"--old", "{cwd_list}"}}, Tool{ID: "a", Name: "x"})
	require.Equal(t, []string{"--old", "{cwd_list}"}, kept.TrustArgs)
}

func TestValidateBusyRegex(t *testing.T) {
	tool := Tool{ID: "x", Models: []Model{{ID: "m"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "p", IdleMs: 100, BusyRegex: "("}}
	require.ErrorContains(t, validate([]Tool{tool}), "busy_regex")
	tool.Detect.BusyRegex = "esc to cancel"
	require.NoError(t, validate([]Tool{tool}))
}
