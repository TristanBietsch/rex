package meta

import (
	"flag"
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunCompletion prints a shell completion script.
func RunCompletion(args []string) error {
	fs := flag.NewFlagSet("completion", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.ContentFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "completion: shell required (bash|zsh|fish)")
	}
	verbs := strings.Join(append(core.CommandNames(), "--version"), " ")
	var script string
	switch rest[0] {
	case "bash":
		script = fmt.Sprintf(bashCompletion, verbs)
	case "zsh":
		script = fmt.Sprintf(zshCompletion, verbs)
	case "fish":
		script = fmt.Sprintf(fishCompletion, verbs)
	default:
		return core.NewExitError(core.ExitInvalidArgs, "supported shells: bash, zsh, fish")
	}
	return core.Out().Emit(core.Result{Cmd: "completion", Content: script})
}

const bashCompletion = `# rex bash completion. Source this from your ~/.bashrc:
#   source <(rex completion bash)
_rex_complete() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local verbs="%s"
    COMPREPLY=( $(compgen -W "$verbs" -- "$cur") )
}
complete -F _rex_complete rex
`

const zshCompletion = `# rex zsh completion. Source this from your ~/.zshrc:
#   source <(rex completion zsh)
_rex() {
    local -a verbs
    verbs=(%s)
    _describe 'verb' verbs
}
compdef _rex rex
`

const fishCompletion = `# rex fish completion. Source this:
#   rex completion fish | source
complete -c rex -f
complete -c rex -n "__fish_use_subcommand" -a "%s"
`
